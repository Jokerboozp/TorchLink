package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// canary sends one report per second from the first device in -devices and
// polls the raw message until it is PARSED, printing CSV:
// time,ingest_status,ingest_ms,e2e_parsed_ms. Polling reads raw message
// details, which for ClickHouse-stored messages is expensive; keep at most 20
// messages in flight and poll once per second.
func canary() {
	d := loadDevices()[0]
	c := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}
	fmt.Println("time,ingest_status,ingest_ms,e2e_parsed_ms")
	sem := make(chan struct{}, 20)
	for {
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			st := time.Now()
			ts := st.Format("15:04:05")
			b, _ := json.Marshal(map[string]any{"id": "canary-" + rid(), "timestamp": st.UnixMilli(), "data": map[string]any{"temperature": 20, "stressAlarm": 0}})
			req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/device-ingest/standard/%s/%s/%s/property", *base, d.Tenant, d.Product, d.Device), bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Device-Key", d.Key)
			req.Header.Set("X-Device-Secret", d.Secret)
			resp, err := c.Do(req)
			if err != nil {
				fmt.Printf("%s,ERR,%d,\n", ts, time.Since(st).Milliseconds())
				return
			}
			var v struct {
				MessageID string `json:"messageId"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			ingest := time.Since(st).Milliseconds()
			if resp.StatusCode != http.StatusAccepted {
				fmt.Printf("%s,%d,%d,\n", ts, resp.StatusCode, ingest)
				return
			}
			for time.Since(st) < 120*time.Second {
				r, _ := http.NewRequest(http.MethodGet, *base+"/api/v1/raw-messages/"+v.MessageID, nil)
				r.Header.Set("Authorization", "Bearer "+*token)
				if resp, err := c.Do(r); err == nil {
					var x struct {
						ParseStatus string `json:"parseStatus"`
					}
					_ = json.NewDecoder(resp.Body).Decode(&x)
					resp.Body.Close()
					if x.ParseStatus == "PARSED" {
						fmt.Printf("%s,202,%d,%d\n", ts, ingest, time.Since(st).Milliseconds())
						return
					}
				}
				time.Sleep(time.Second)
			}
			fmt.Printf("%s,202,%d,TIMEOUT120s\n", ts, ingest)
		}()
		time.Sleep(time.Second)
	}
}
