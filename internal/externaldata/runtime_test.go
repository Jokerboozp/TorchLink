package externaldata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/externaldata"
)

func TestRuntimeCrossInterfaceDedupOrderingAndRecovery(t *testing.T) {
	ctx := context.Background()
	var delivered []externaldata.Event
	var prior []externaldata.Result
	deliver := func(_ context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint, event externaldata.Event, _ externaldata.Binding, previous externaldata.Result) (externaldata.Result, error) {
		delivered = append(delivered, event)
		prior = append(prior, previous)
		return externaldata.Result{AlarmID: "alarm-stable", CameraID: "camera-internal"}, nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"eventId":"fire-1","time":1000,"version":1,"status":"ACTIVE","text":"火警","camera":"vendor-camera"}]}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, deliver, u.Host)
	push := runtimeEndpoint(t, s, source, externaldata.Endpoint{Kind: "video_alarm"})
	mapping := runtimeMapping()
	mapping.ItemsPath = "items"
	pull := runtimeEndpoint(t, s, source, externaldata.Endpoint{Kind: "video_alarm", Mode: "pull", URL: server.URL, Mapping: mapping})
	if _, err := s.SaveBinding(ctx, testTenant, externaldata.Binding{SourceID: source.ID, Kind: "camera", ExternalID: "vendor-camera", TargetID: "camera-internal"}); err != nil {
		t.Fatal(err)
	}
	runtimeReceive(t, s, push, `{"eventId":"fire-1","time":1000,"version":1,"status":"ACTIVE","text":"火警","camera":"vendor-camera"}`)
	runtimeStep(t, s, "record")
	if _, err := s.Pull(ctx, testTenant, pull, 1, 2000); err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "job")
	runtimeStep(t, s, "record")
	if len(delivered) != 1 {
		t.Fatalf("same push/pull event delivered %d times", len(delivered))
	}
	statuses := map[string]int{}
	for _, e := range runtimeEntries(t, s, "record") {
		statuses[e.Status]++
	}
	if statuses["PROCESSED"] != 1 || statuses["DUPLICATE"] != 1 {
		t.Fatal(statuses)
	}
	runtimeReceive(t, s, push, `{"eventId":"fire-1","time":3000,"version":3,"status":"RECOVERED","text":"恢复","camera":"vendor-camera"}`)
	runtimeStep(t, s, "record")
	if len(delivered) != 2 || delivered[1].Status != "RECOVERED" || prior[1].AlarmID != "alarm-stable" {
		t.Fatalf("recovery lost prior alarm: %+v %+v", delivered, prior)
	}
	runtimeReceive(t, s, push, `{"eventId":"fire-1","time":2000,"version":2,"status":"ACTIVE","text":"晚到旧告警","camera":"vendor-camera"}`)
	runtimeStep(t, s, "record")
	if len(delivered) != 2 {
		t.Fatal("out-of-order event overwrote recovery")
	}
	statuses = map[string]int{}
	for _, e := range runtimeEntries(t, s, "record") {
		statuses[e.Status]++
	}
	if statuses["IGNORED"] != 1 {
		t.Fatal(statuses)
	}
	// The same version with different contents needs human resolution.
	runtimeReceive(t, s, push, `{"eventId":"fire-1","time":3000,"version":3,"status":"ACTIVE","text":"冲突内容","camera":"vendor-camera"}`)
	runtimeStep(t, s, "record")
	if len(delivered) != 2 {
		t.Fatal("conflicting event delivered")
	}
	statuses = map[string]int{}
	for _, e := range runtimeEntries(t, s, "record") {
		statuses[e.Status]++
	}
	if statuses["CONFLICT"] != 1 {
		t.Fatal(statuses)
	}
}

func TestRuntimeDeliveryRetryKeepsPriorVersionFence(t *testing.T) {
	var delivered []int64
	failNew := true
	s, source := runtimeFixture(t, func(_ context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint, event externaldata.Event, _ externaldata.Binding, previous externaldata.Result) (externaldata.Result, error) {
		if event.Version == 3 && failNew {
			return externaldata.Result{}, errors.New("injected business failure")
		}
		delivered = append(delivered, event.Version)
		return externaldata.Result{AlarmID: "same-alarm"}, nil
	})
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{})
	runtimeReceive(t, s, ep, `{"eventId":"one","time":2000,"version":2,"text":"已保存"}`)
	runtimeStep(t, s, "record")
	runtimeReceive(t, s, ep, `{"eventId":"one","time":3000,"version":3,"text":"投递失败后重试"}`)
	runtimeStep(t, s, "record")
	var retry externaldata.Entry
	for _, e := range runtimeEntries(t, s, "record") {
		if e.Status == "RETRY" {
			retry = e
		}
	}
	if retry.ID == "" {
		t.Fatal("failed delivery not queued for retry")
	}
	runtimeReceive(t, s, ep, `{"eventId":"one","time":1000,"version":1,"text":"晚到更旧数据"}`)
	runtimeStep(t, s, "record")
	if !reflect.DeepEqual(delivered, []int64{2}) {
		t.Fatalf("failed newer event removed prior version fence: %v", delivered)
	}
	failNew = false
	runtimeDue(t, s, "record", retry.ID)
	runtimeStep(t, s, "record")
	if !reflect.DeepEqual(delivered, []int64{2, 3}) {
		t.Fatalf("retry delivery: %v", delivered)
	}
	completed, err := s.Store.Get(context.Background(), testTenant, "record", retry.ID)
	if err != nil || completed.Status != "PROCESSED" || runtimeBody[externaldata.Record](t, completed).Error != "" {
		t.Fatalf("retry not completed: %+v %v", completed, err)
	}
}

func TestRuntimeUnknownBindingAndAuthorizationRecheck(t *testing.T) {
	ctx := context.Background()
	delivered := 0
	s, source := runtimeFixture(t, func(_ context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint, event externaldata.Event, _ externaldata.Binding, previous externaldata.Result) (externaldata.Result, error) {
		delivered++
		return externaldata.Result{AlarmID: "alarm"}, nil
	})
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Kind: "video_alarm"})
	runtimeReceive(t, s, ep, `{"eventId":"unknown","time":1,"text":"未知摄像头","camera":"external-camera"}`)
	runtimeStep(t, s, "record")
	e := runtimeEntries(t, s, "record")[0]
	if e.Status != "WAITING_BINDING" || delivered != 0 {
		t.Fatal("unknown binding generated alarm")
	}
	if _, err := s.SaveBinding(ctx, testTenant, externaldata.Binding{SourceID: source.ID, Kind: "camera", ExternalID: "external-camera", TargetID: "camera-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Retry(ctx, testTenant, "record", e.ID, e.Revision, false); err != nil {
		t.Fatal(err)
	}
	s.Authorize = func(context.Context, string, externaldata.Source, externaldata.Endpoint) (context.Context, error) {
		return nil, errors.New("permission revoked")
	}
	runtimeStep(t, s, "record")
	e = runtimeEntries(t, s, "record")[0]
	if e.Status != "FAILED" || delivered != 0 {
		t.Fatal("revoked permission still delivered")
	}
	s.Authorize = func(ctx context.Context, _ string, _ externaldata.Source, _ externaldata.Endpoint) (context.Context, error) {
		return ctx, nil
	}
	if _, err := s.Retry(ctx, testTenant, "record", e.ID, e.Revision, false); err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "record")
	if delivered != 1 {
		t.Fatal("binding retry did not deliver")
	}
}

func TestRuntimeDefaultPageAdvancesAndRestarts(t *testing.T) {
	var mu sync.Mutex
	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		pages = append(pages, page)
		mu.Unlock()
		if page > 2 {
			fmt.Fprint(w, `{"items":[]}`)
			return
		}
		fmt.Fprintf(w, `{"items":[{"eventId":"page-%d","time":%d,"text":"告警"}]}`, page, page)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, nil, u.Host)
	mapping := runtimeMapping()
	mapping.ItemsPath = "items"
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL, Mapping: mapping, Pagination: externaldata.Pagination{Mode: "page", PageSize: 1}})
	job, err := s.Pull(context.Background(), testTenant, ep, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "job")
	// Reconstruct the service while retaining its durable repository.
	s = runtimeService(t, s.Store, nil)
	for i := 0; i < 4; i++ {
		runtimeExpireRequestWindow(t, s)
		worked, err := s.Step(context.Background(), "job", "restarted-worker")
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(pages, []int{1, 2, 3}) {
		t.Fatalf("page cursor repeated or skipped after restart: %v", pages)
	}
	finished, err := s.Store.Get(context.Background(), testTenant, "job", job.ID)
	j := runtimeBody[externaldata.Job](t, finished)
	if err != nil || finished.Status != "COMPLETED" || j.Pages != 3 || j.Received != 2 {
		t.Fatalf("page progress: %+v %+v %v", finished, j, err)
	}
}

func TestRuntimeScheduleManualIsolationAndChangedCursor(t *testing.T) {
	ctx := context.Background()
	requests := make(chan string, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query().Get("cursor")
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"items":[{"eventId":"first","time":1,"text":"一"}],"next":"page-two"}`)
		} else {
			fmt.Fprint(w, `{"items":[{"eventId":"last","time":2,"text":"二"}],"next":""}`)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, nil, u.Host)
	mapping := runtimeMapping()
	mapping.ItemsPath = "items"
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL, Mapping: mapping, StartAt: 1000, IntervalSeconds: 60, Pagination: externaldata.Pagination{Mode: "cursor", PageSize: 1, NextPath: "next"}})
	runtimeStep(t, s, "schedule")
	scheduled := runtimeBody[map[string]any](t, runtimeEntries(t, s, "schedule")[0])
	autoID, _ := scheduled["jobId"].(string)
	if autoID == "" || scheduled["watermark"] != float64(1000) {
		t.Fatal(scheduled)
	}
	manual, err := s.Pull(ctx, testTenant, ep, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Only the manual task executes; the automatic task remains associated
	// with the original watermark and cannot be advanced by this completion.
	auto, err := s.Store.Get(ctx, testTenant, "job", autoID)
	if err != nil {
		t.Fatal(err)
	}
	auto.DueAt = time.Now().Add(time.Hour).UnixMilli()
	if _, err = s.Store.Put(ctx, auto, auto.Revision); err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "job")
	runtimeStep(t, s, "job")
	manualDone, err := s.Store.Get(ctx, testTenant, "job", manual.ID)
	if err != nil || manualDone.Status != "COMPLETED" {
		t.Fatal("manual pull incomplete", err)
	}
	runtimeDue(t, s, "schedule", ep.ID)
	runtimeStep(t, s, "schedule")
	afterManual := runtimeBody[map[string]any](t, runtimeEntries(t, s, "schedule")[0])
	if afterManual["jobId"] != autoID || afterManual["watermark"] != float64(1000) {
		t.Fatalf("manual pull changed schedule: %v", afterManual)
	}
	runtimeDue(t, s, "job", autoID)
	runtimeStep(t, s, "job")
	auto, err = s.Store.Get(ctx, testTenant, "job", autoID)
	j := runtimeBody[externaldata.Job](t, auto)
	if err != nil || j.Cursor != "page-two" || j.Pages != 1 {
		t.Fatalf("cursor not persisted: %+v %v", j, err)
	}
	// Updating a mapping cannot reinterpret the already saved vendor cursor.
	ep.Name = "更新配置"
	ep, err = s.SaveEndpoint(ctx, testTenant, ep)
	if err != nil {
		t.Fatal(err)
	}
	s = runtimeService(t, s.Store, nil)
	runtimeStep(t, s, "job")
	auto, err = s.Store.Get(ctx, testTenant, "job", autoID)
	j = runtimeBody[externaldata.Job](t, auto)
	if err != nil || auto.Status != "FAILED" || j.Cursor != "page-two" || !strings.Contains(j.Error, "配置") {
		t.Fatalf("changed config reused cursor: %+v %+v %v", auto, j, err)
	}
	if len(requests) != 3 {
		t.Fatalf("configuration change unexpectedly fetched an old cursor: %d requests", len(requests))
	}
	// The failed old configuration remains inspectable, but it must not pin
	// the automatic plan forever. New work resumes from the same watermark.
	for i := 0; i < 3; i++ {
		runtimeDue(t, s, "schedule", ep.ID)
		runtimeStep(t, s, "schedule")
	}
	var replacement externaldata.Entry
	for _, e := range runtimeEntries(t, s, "job") {
		job := runtimeBody[externaldata.Job](t, e)
		if !job.Manual && job.ConfigRevision == ep.Revision && e.Status == "PENDING" {
			replacement = e
		}
	}
	if replacement.ID == "" || replacement.ID == autoID {
		t.Fatal("changed configuration permanently pinned automatic schedule to the failed old cursor")
	}
	newJob := runtimeBody[externaldata.Job](t, replacement)
	if newJob.From != 1000 || newJob.Cursor != "" || newJob.Pages != 0 {
		t.Fatalf("replacement skipped unconsumed interval or reused old cursor: %+v", newJob)
	}
	runtimeStep(t, s, "job")
	runtimeStep(t, s, "job")
	runtimeDue(t, s, "schedule", ep.ID)
	runtimeStep(t, s, "schedule")
	finishedSchedule := runtimeBody[map[string]any](t, runtimeEntries(t, s, "schedule")[0])
	if finishedSchedule["watermark"] != float64(newJob.To) {
		t.Fatalf("completed replacement did not advance watermark: %v", finishedSchedule)
	}
}

func TestRuntimePullKeepsMalformedListResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"actualItems":[{"eventId":"raw-before-mapping","time":1,"text":"保留失败响应"}]}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, nil, u.Host)
	mapping := runtimeMapping()
	mapping.ItemsPath = "wrongItems"
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL, Mapping: mapping})
	if _, err := s.Pull(context.Background(), testTenant, ep, 1, 2); err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "job")
	if len(runtimeEntries(t, s, "receipt")) != 1 || len(runtimeEntries(t, s, "record")) != 1 {
		t.Fatal("pull discarded a received response before mapping validation")
	}
	r := runtimeBody[externaldata.Record](t, runtimeEntries(t, s, "record")[0])
	if !strings.Contains(string(r.Raw), "raw-before-mapping") || r.Error == "" {
		t.Fatalf("mapping failure lost raw response or error: %+v", r)
	}
	job := runtimeBody[externaldata.Job](t, runtimeEntries(t, s, "job")[0])
	if job.Pages != 0 || job.Cursor != "" {
		t.Fatalf("invalid response advanced checkpoint: %+v", job)
	}
}

func TestRuntimeHTTPFailureThenIdenticalSuccessDelivers(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"eventId": "same-http-body", "time": 1, "text": "重试成功"})
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	delivered := 0
	s, source := runtimeFixture(t, func(context.Context, string, externaldata.Source, externaldata.Endpoint, externaldata.Event, externaldata.Binding, externaldata.Result) (externaldata.Result, error) {
		delivered++
		return externaldata.Result{}, nil
	}, u.Host)
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL})
	job, err := s.Pull(context.Background(), testTenant, ep, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	runtimeStep(t, s, "job")
	failed, err := s.Store.Get(context.Background(), testTenant, "job", job.ID)
	if err != nil || failed.Status != "RETRY" || len(runtimeEntries(t, s, "record")) != 1 {
		t.Fatalf("failed response was not retained for retry: %+v %v", failed, err)
	}
	runtimeDue(t, s, "job", job.ID)
	runtimeStep(t, s, "job")
	if len(runtimeEntries(t, s, "record")) != 2 {
		t.Fatal("the previous HTTP error response blocked a later successful identical payload")
	}
	runtimeStep(t, s, "record")
	if delivered != 1 || calls.Load() != 2 {
		t.Fatalf("HTTP retry delivery=%d calls=%d", delivered, calls.Load())
	}
}
