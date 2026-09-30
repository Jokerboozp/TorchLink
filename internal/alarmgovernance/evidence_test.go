package alarmgovernance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"os"
	"testing"
)

type testArchive struct {
	objects       map[string][]byte
	puts, deletes int
	putErr        error
	afterPut      func()
	afterGet      func()
}

func newTestArchive() *testArchive { return &testArchive{objects: map[string][]byte{}} }
func (a *testArchive) PutObject(_ context.Context, bucket, key string, r io.Reader, _ int64, _ string) (string, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return "", e
	}
	a.objects[bucket+"/"+key] = b
	a.puts++
	if a.afterPut != nil {
		a.afterPut()
	}
	return key, a.putErr
}
func (a *testArchive) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	b, ok := a.objects[bucket+"/"+key]
	if !ok {
		return nil, os.ErrNotExist
	}
	if a.afterGet != nil {
		a.afterGet()
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (a *testArchive) DeleteObject(_ context.Context, bucket, key string) error {
	delete(a.objects, bucket+"/"+key)
	a.deletes++
	return nil
}
func (a *testArchive) Health(context.Context) error { return nil }

var pdfEvidence = []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")

func TestAttachmentMIMEBoundsIdempotencyAndImmutableReferenceWithdrawal(t *testing.T) {
	f := setup(t)
	v := f.newVerification(t)
	archive := newTestArchive()
	for _, q := range []struct {
		name string
		data []byte
	}{{"evidence.jpg", pdfEvidence}, {"evidence.html", []byte("<html>unsafe</html>")}, {"evidence.pdf", make([]byte, AttachmentMax+1)}} {
		if _, e := f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, q.name, "invalid-"+q.name, q.data); !errors.Is(e, model.ErrGovernanceInvalid) {
			t.Fatalf("invalid MIME/size accepted: %v", e)
		}
	}
	if archive.puts != 0 {
		t.Fatal("invalid input uploaded")
	}
	attachment, e := f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, "../safe.pdf", "attachment", pdfEvidence)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := model.GovernanceBody[model.GovernanceAttachment](attachment)
	if body.Name != "safe.pdf" || body.StorageKey == "" || body.SHA256 == "" {
		t.Fatalf("uncontrolled upload metadata %+v", body)
	}
	again, e := f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, "safe.pdf", "attachment", pdfEvidence)
	if e != nil || again.ID != attachment.ID || archive.puts != 1 {
		t.Fatalf("idempotency failed %+v %v", again, e)
	}
	if _, e = f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, "safe.pdf", "attachment", append(append([]byte{}, pdfEvidence...), 'x')); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatalf("key hash mismatch accepted: %v", e)
	}
	public, data, e := f.s.DownloadAttachment(f.ctx, f.a, archive, attachment.ID)
	if e != nil || public.StorageKey != "" || !bytes.Equal(data, pdfEvidence) {
		t.Fatalf("download mismatch %+v %v", public, e)
	}
	vbody, _ := model.GovernanceBody[model.FieldVerification](v)
	vbody.AttachmentIDs = []string{attachment.ID}
	v = f.must(t, command(v.Kind, v.ID, "", "update", vbody, v.Version))
	v = f.must(t, command(v.Kind, v.ID, "", "confirm", map[string]any{}, v.Version))
	withdrawn, e := f.s.WithdrawAttachment(f.ctx, f.a, attachment.ID, attachment.Version, "撤回引用，保留原始制品")
	if e != nil {
		t.Fatal(e)
	}
	metadata, _ := model.GovernanceBody[model.GovernanceAttachment](withdrawn)
	if metadata.SHA256 != body.SHA256 || metadata.Availability != "WITHDRAWN" || metadata.AvailabilityReason == "" || archive.deletes != 0 {
		t.Fatal("formal attachment was physically removed or audit lost")
	}
	if _, _, e = f.s.DownloadAttachment(f.ctx, f.a, archive, attachment.ID); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("withdrawn attachment downloadable: %v", e)
	}
}
func TestAttachmentRegistrationAndRemoteFailureCompensation(t *testing.T) {
	for _, remoteFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked-after-upload", true: "remote-failure"}[remoteFailure], func(t *testing.T) {
			f := setup(t)
			v := f.newVerification(t)
			archive := newTestArchive()
			if remoteFailure {
				archive.putErr = errors.New("remote upload failed after partial write")
			} else {
				archive.afterPut = func() {
					f.s.ResolveTx = func(ports.AlarmGovernanceTx, Actor) (Actor, error) { return Actor{}, ErrForbidden }
				}
			}
			if _, e := f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, "evidence.pdf", "failed", pdfEvidence); e == nil {
				t.Fatal("failure accepted")
			}
			if len(archive.objects) != 0 || archive.deletes != 1 {
				t.Fatalf("orphan not compensated: %d %d", len(archive.objects), archive.deletes)
			}
			var total int
			_ = f.repo.GovernanceRead(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error {
				_, total, _ = tx.List(model.GovernanceFilter{Kind: model.GovernanceAttachmentKind, AllDevices: true})
				return nil
			})
			if total != 0 {
				t.Fatal("failed upload registered metadata")
			}
		})
	}
}
func TestDamagedAndMissingAttachmentPersistAvailabilityWithoutDestroyingHash(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "damaged", true: "missing"}[missing], func(t *testing.T) {
			f := setup(t)
			v := f.newVerification(t)
			archive := newTestArchive()
			d, e := f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, "evidence.pdf", "damage", pdfEvidence)
			if e != nil {
				t.Fatal(e)
			}
			original, _ := model.GovernanceBody[model.GovernanceAttachment](d)
			if missing {
				delete(archive.objects, AttachmentBucket+"/"+original.StorageKey)
			} else {
				archive.objects[AttachmentBucket+"/"+original.StorageKey] = []byte("changed")
			}
			if _, _, e = f.s.DownloadAttachment(f.ctx, f.a, archive, d.ID); e == nil {
				t.Fatal("unverifiable evidence returned")
			}
			latest, e := f.s.Get(f.ctx, f.a, d.Kind, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			body, _ := model.GovernanceBody[model.GovernanceAttachment](latest)
			expected := "DAMAGED"
			if missing {
				expected = "MISSING"
			}
			if body.Availability != expected || latest.Version <= d.Version || body.SHA256 != original.SHA256 || body.StorageKey != original.StorageKey || body.AvailabilityReason == "" {
				t.Fatalf("availability provenance lost: %+v", body)
			}
			events, total, e := f.s.List(f.ctx, f.a, model.GovernanceFilter{Kind: model.GovernanceEventKind, Limit: 100})
			if e != nil || total < 2 || len(events) < 2 {
				t.Fatalf("availability event absent %d %v", total, e)
			}
		})
	}
}
func TestDownloadRejectsConcurrentWithdrawal(t *testing.T) {
	f := setup(t)
	v := f.newVerification(t)
	archive := newTestArchive()
	d, e := f.s.UploadAttachment(f.ctx, f.a, archive, v.Kind, v.ID, "evidence.pdf", "concurrent", pdfEvidence)
	if e != nil {
		t.Fatal(e)
	}
	archive.afterGet = func() {
		archive.afterGet = nil
		if _, e := f.s.WithdrawAttachment(f.ctx, f.a, d.ID, d.Version, "读取期间撤回"); e != nil {
			t.Fatal(e)
		}
	}
	if _, data, e := f.s.DownloadAttachment(f.ctx, f.a, archive, d.ID); !errors.Is(e, model.ErrGovernanceConflict) || len(data) > 0 {
		t.Fatalf("withdrawn data delivered: %d %v", len(data), e)
	}
}
func TestBusinessLinkWholeMembershipIdempotencyAndFrozenReportProvenance(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	source := BusinessSource{Version: 2, DeviceIDs: []string{"device-a", "device-b"}, Summary: "共享设备现场资料", Status: "AVAILABLE"}
	resolver := func(context.Context, Actor, string, string) (BusinessSource, error) { return source, nil }
	q := model.GovernanceBusinessLink{TargetKind: "DUTY_RECORD", TargetID: "record", TargetVersion: 2, Relation: "现场核查依据"}
	link, e := f.s.CreateBusinessLink(f.ctx, f.a, c.ID, q, "link", resolver)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.s.CreateBusinessLink(f.ctx, f.a, c.ID, q, "link", resolver)
	if e != nil || again.ID != link.ID {
		t.Fatalf("link idempotency failed %v", e)
	}
	q.Relation = "different"
	if _, e = f.s.CreateBusinessLink(f.ctx, f.a, c.ID, q, "link", resolver); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatalf("link body hash conflict absent %v", e)
	}
	limited := Actor{TenantID: f.a.TenantID, Username: "limited", Permissions: []string{"menu:alarmGovernance", "menu:devices", "menu:alarms", "action:alarmGovernance:record"}, DeviceIDs: []string{"device-a"}}
	q.Relation = "current"
	if _, e = f.s.CreateBusinessLink(f.ctx, limited, c.ID, q, "partial", resolver); !errors.Is(e, ErrForbidden) {
		t.Fatalf("partial shared source accepted %v", e)
	}
	reportRaw, _ := json.Marshal(model.GovernanceReport{CaseID: c.ID, Resources: []model.GovernanceDocument{link}})
	report := model.GovernanceDocument{Kind: model.GovernanceReportKind, ID: "report", CaseID: c.ID, DeviceIDs: []string{"device-a"}, Body: reportRaw}
	source.DeviceIDs = []string{"device-a"}
	if e = f.s.AuthorizeProvenance(f.ctx, limited, report, resolver); !errors.Is(e, ErrForbidden) {
		t.Fatalf("historical shared-summary member forgotten: %v", e)
	}
	emptyRaw, _ := json.Marshal(model.GovernanceReport{CaseID: c.ID})
	emptyReport := report
	emptyReport.Body = emptyRaw
	deniedResolver := func(context.Context, Actor, string, string) (BusinessSource, error) {
		return BusinessSource{}, ErrForbidden
	}
	if e = f.s.AuthorizeProvenance(f.ctx, f.a, emptyReport, deniedResolver); e != nil {
		t.Fatalf("later case link changed old report scope: %v", e)
	}
}
func TestListingRejectsUnreadableSourceOnLaterPageBeforeDisclosingTotal(t *testing.T) {
	f := setup(t)
	for i := 0; i < 3; i++ {
		f.newVerification(t)
	}
	docs, _, e := f.s.List(f.ctx, f.a, model.GovernanceFilter{Kind: model.GovernanceVerificationKind, Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	hidden := docs[2].ID
	f.s.AuthorizeSource = func(_ context.Context, _ Actor, d model.GovernanceDocument) error {
		if d.ID == hidden {
			return ErrForbidden
		}
		return nil
	}
	items, total, e := f.s.List(f.ctx, f.a, model.GovernanceFilter{Kind: model.GovernanceVerificationKind, Limit: 1})
	if !errors.Is(e, ErrForbidden) || items != nil || total != 0 {
		t.Fatalf("later-page hidden source count disclosed: %d %v", total, e)
	}
}
