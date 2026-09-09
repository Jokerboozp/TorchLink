package main

import (
	"context"
	"flag"
	"iot-platform/internal/config"
	"iot-platform/internal/edgeupgrade"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	env := flag.String("env-file", "", "本地私有 Agent 环境文件，必填")
	bootstrap := flag.String("bootstrap", "", "首次启动的本地 iot-edge-agent 可执行文件")
	directory := flag.String("program-dir", "", "程序和升级日志目录，必须与 Agent 数据目录不同")
	policy := flag.String("catalog-policy", "", "本地可信签名目录策略；空值禁用远程升级")
	flag.Parse()
	if *env == "" {
		slog.Error("必须指定 --env-file")
		os.Exit(1)
	}
	if err := config.LoadEnvFile(*env); err != nil {
		slog.Error("读取配置失败", "error", err)
		os.Exit(1)
	}
	launcher, err := edgeupgrade.New(edgeupgrade.Options{URL: os.Getenv("IOT_EDGE_PLATFORM_URL"), TenantID: os.Getenv("IOT_EDGE_TENANT_ID"), NodeID: os.Getenv("IOT_EDGE_NODE_ID"), Secret: os.Getenv("IOT_EDGE_SECRET"), AllowHTTP: os.Getenv("IOT_EDGE_ALLOW_HTTP") == "true", DataDir: *directory, BootstrapBinary: *bootstrap, AgentEnvFile: *env, CatalogPolicy: *policy, Output: os.Stderr})
	if err != nil {
		slog.Error("初始化启动器失败", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = launcher.Run(ctx)
	stopped := ctx.Err() != nil
	cancel()
	_ = launcher.Close()
	if err != nil && !stopped {
		slog.Error("启动器退出", "error", err)
		os.Exit(1)
	}
}
