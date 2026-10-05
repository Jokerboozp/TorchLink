package minioadapter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestZZRustFS(t *testing.T) {
	if os.Getenv("RFS") == "" {
		t.Skip()
	}
	a, err := New(os.Getenv("RFS"), os.Getenv("RFS_U"), os.Getenv("RFS_P"), false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		start := time.Now()
		_, err = a.PutObject(ctx, "iot-site-plans", fmt.Sprintf("probe/%d.txt", i), strings.NewReader(strings.Repeat("x", 4096)), 4096, "text/plain")
		cancel()
		t.Logf("put %d took %v err=%v", i, time.Since(start).Round(time.Millisecond), err)
	}
}
