package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/parser"
)

func TestDoReqRequiresCompleteResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length string
		wantOK bool
	}{
		{"complete", "3", true},
		{"truncated", "100", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", tc.length)
				_, _ = w.Write([]byte("pdf"))
			}))
			defer s.Close()
			ok, code, size := doReq(context.Background(), s.Client(), http.MethodGet, s.URL, nil, nil)
			if ok != tc.wantOK || size != 3 {
				t.Fatalf("ok=%v code=%s size=%d", ok, code, size)
			}
		})
	}
}

func TestReadGBFrameUDP(t *testing.T) {
	valid := parser.BuildGB26875RegistrationFrame(1, [6]byte{1}, time.Now())
	for _, frame := range [][]byte{valid, valid[:10]} {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.Dial("udp", listener.LocalAddr().String())
		if err != nil {
			listener.Close()
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, _ = listener.WriteTo(frame, conn.LocalAddr())
		got, err := readGBFrame(conn)
		conn.Close()
		listener.Close()
		if len(frame) == len(valid) && (err != nil || len(got) != len(valid)) {
			t.Fatalf("complete datagram: length=%d err=%v", len(got), err)
		}
		if len(frame) < 30 && err == nil {
			t.Fatal("accepted truncated datagram")
		}
	}
}

func TestMetricsScrapeDoesNotConsumeLoadWindow(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write([]byte("raw_archive_success_total 0\nparse_success_total 0\n"))
	}))
	defer s.Close()
	oldBase, oldLevels, oldRates, oldProbe, oldOut := *base, *levelsS, *ratesS, *probe, *outFile
	oldStep, oldStop := *stepDur, *stopErr
	defer func() {
		*base, *levelsS, *ratesS, *probe, *outFile = oldBase, oldLevels, oldRates, oldProbe, oldOut
		*stepDur, *stopErr = oldStep, oldStop
	}()
	*base, *levelsS, *ratesS, *probe, *outFile = s.URL, "1", "", "", ""
	*stepDur, *stopErr = 20*time.Millisecond, -1
	levels := runLevels(func(context.Context, *http.Client, int) (bool, string, int64) {
		time.Sleep(time.Millisecond)
		return true, "ok", 0
	})
	if len(levels) != 1 || levels[0].OK == 0 || levels[0].DurationSec < stepDur.Seconds() {
		t.Fatalf("metrics collection consumed the load window: %+v", levels)
	}
}

func TestValidCounterSamples(t *testing.T) {
	start := map[string]float64{"raw_archive_success_total": 10, "parse_success_total": 8, "alarm_trigger_total": 2}
	for _, tc := range []struct {
		name string
		end  map[string]float64
		want bool
	}{
		{"complete", map[string]float64{"raw_archive_success_total": 11, "parse_success_total": 9, "alarm_trigger_total": 2}, true},
		{"missing parser", map[string]float64{"raw_archive_success_total": 11, "alarm_trigger_total": 2}, false},
		{"reset parser", map[string]float64{"raw_archive_success_total": 11, "parse_success_total": 1, "alarm_trigger_total": 2}, false},
		{"reset alarms", map[string]float64{"raw_archive_success_total": 11, "parse_success_total": 9, "alarm_trigger_total": 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validCounterSamples(start, tc.end); got != tc.want {
				t.Fatalf("valid=%v, want %v", got, tc.want)
			}
		})
	}
}
