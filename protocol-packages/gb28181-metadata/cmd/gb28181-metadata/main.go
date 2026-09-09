package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	gb "iot-platform/protocol-packages/gb28181-metadata"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func run() error {
	path := flag.String("config", "", "本机私有 JSON 配置文件（含设备密码，仅节点保存）")
	flag.Parse()
	info, err := os.Stat(*path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return errors.New("configuration must be a private regular file within 1 MiB")
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return errors.New("read configuration failed")
	}
	var cfg struct {
		SIP      gb.Config `json:"sip"`
		Platform struct {
			URL, TenantID, NodeID, Secret, DataDir string
			AllowHTTP                              bool
		} `json:"platform"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return errors.New("invalid configuration JSON")
	}
	registrar, err := gb.New(cfg.SIP)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	u := gb.Uploader{URL: cfg.Platform.URL, TenantID: cfg.Platform.TenantID, NodeID: cfg.Platform.NodeID, Secret: cfg.Platform.Secret, DataDir: cfg.Platform.DataDir, AllowHTTP: cfg.Platform.AllowHTTP}
	if err := u.Init(ctx); err != nil {
		return err
	}
	if err := u.Restore(registrar); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- registrar.Serve(ctx) }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	lastAuthenticated := time.Now()
	for {
		select {
		case <-ctx.Done():
			<-done
			return nil
		case err := <-done:
			return err
		case <-ticker.C:
			err := u.Upload(ctx, registrar.Snapshot())
			if errors.Is(err, gb.ErrNodeUnauthorized) {
				cancel()
				<-done
				return err
			}
			if err != nil {
				slog.Warn("目录上传未确认，保留本地快照", "error", err)
			} else {
				lastAuthenticated = time.Now()
			}
			if time.Since(lastAuthenticated) > 24*time.Hour {
				cancel()
				<-done
				return errors.New("platform authentication could not be refreshed for 24 hours")
			}
		}
	}
}
func main() {
	if err := run(); err != nil {
		slog.Error("GB28181 元数据节点停止", "error", err)
		os.Exit(1)
	}
}
