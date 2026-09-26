package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var (
	provCount  = flag.Int("count", 1000, "provision: number of devices")
	provStart  = flag.Int("start", 0, "provision: first device index")
	provPar    = flag.Int("par", 16, "provision: parallel requests")
	provProd   = flag.String("product", "", "provision: existing standard-protocol product ID")
	provPrefix = flag.String("prefix", "st-dev", "provision: device ID prefix")
	provTenant = flag.String("tenant", "tenant_001", "provision: tenant recorded with the credentials")
)

// provision enrolls devices through POST /api/v1/onboarding and appends their
// credentials to -devices (mode 0600). The measured rate is the onboarding capacity.
func provision() {
	c := httpClient(*provPar)
	var existing []Device
	if b, err := os.ReadFile(*devFile); err == nil {
		_ = json.Unmarshal(b, &existing)
	}
	out := make([]Device, *provCount)
	r := &rec{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, *provPar)
	st := time.Now()
	var done atomic.Int64
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			fmt.Printf("  provisioned %d/%d (%.1f/s)\n", done.Load(), *provCount, float64(done.Load())/time.Since(st).Seconds())
		}
	}()
	for i := 0; i < *provCount; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			id := fmt.Sprintf("%s-%06d", *provPrefix, *provStart+i)
			body, _ := json.Marshal(map[string]any{"requestId": "req-" + id, "productId": *provProd, "device": map[string]any{"id": id, "name": "压测设备 " + id}, "connection": map[string]any{"mode": "standard"}})
			for attempt := 0; attempt < 5; attempt++ {
				req, _ := http.NewRequest(http.MethodPost, *base+"/api/v1/onboarding", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+*token)
				s := time.Now()
				resp, err := c.Do(req)
				if err != nil {
					r.add(float64(time.Since(s).Microseconds())/1000, false, shortErr(err.Error()), 0)
					time.Sleep(time.Second)
					continue
				}
				var v struct {
					Credential struct {
						AccessKey string `json:"accessKey"`
						Secret    string `json:"secret"`
					} `json:"credential"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&v)
				resp.Body.Close()
				ok := resp.StatusCode == http.StatusCreated && v.Credential.Secret != ""
				r.add(float64(time.Since(s).Microseconds())/1000, ok, strconv.Itoa(resp.StatusCode), 0)
				if ok {
					out[i] = Device{*provTenant, *provProd, id, v.Credential.AccessKey, v.Credential.Secret}
					done.Add(1)
					return
				}
				time.Sleep(500 * time.Millisecond)
			}
		}(i)
	}
	wg.Wait()
	l := r.level("provision", *provPar, 0, time.Since(st))
	for _, d := range out {
		if d.Secret != "" {
			existing = append(existing, d)
		}
	}
	b, _ := json.Marshal(existing)
	must(os.WriteFile(*devFile, b, 0o600))
	fmt.Printf("[%s] provisioned=%d total_file=%d elapsed=%s rate=%.1f/s p50=%.1f p95=%.1f p99=%.1f codes=%v\n", *name, done.Load(), len(existing), time.Since(st).Round(time.Second), float64(done.Load())/time.Since(st).Seconds(), l.P50, l.P95, l.P99, l.Codes)
	save([]Level{l})
}
