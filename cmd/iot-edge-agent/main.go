package main

import (
	"context"
	"flag"
	"iot-platform/internal/config"
	"iot-platform/internal/edgeagent"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	env := flag.String("env-file", "", "读取节点环境配置文件")
	retryRejected := flag.Bool("retry-rejected", false, "将已修复的拒收数据重新加入发送队列")
	flag.Parse()
	if *env != "" {
		if err := config.LoadEnvFile(*env); err != nil {
			slog.Error("读取配置失败", "error", err)
			os.Exit(1)
		}
	}
	options := edgeagent.Options{AllowCommands: os.Getenv("IOT_EDGE_ALLOW_COMMANDS") == "true", AllowGoWorkers: os.Getenv("IOT_EDGE_ALLOW_GO_WORKERS") == "true", AllowedListenAddresses: strings.Split(os.Getenv("IOT_EDGE_LISTEN_ADDRESSES"), ","), CredentialFile: os.Getenv("IOT_EDGE_CREDENTIAL_FILE"), URL: os.Getenv("IOT_EDGE_PLATFORM_URL"), TenantID: os.Getenv("IOT_EDGE_TENANT_ID"), NodeID: os.Getenv("IOT_EDGE_NODE_ID"), Secret: os.Getenv("IOT_EDGE_SECRET"), DataDir: os.Getenv("IOT_EDGE_DATA_DIR"), AllowedSerialPorts: strings.Split(os.Getenv("IOT_EDGE_SERIAL_PORTS"), ","), AllowedCIDRs: strings.Split(os.Getenv("IOT_EDGE_ALLOWED_CIDRS"), ","), AllowInsecureHTTP: os.Getenv("IOT_EDGE_ALLOW_HTTP") == "true"}
	agent, err := edgeagent.New(options, slog.Default())
	if err != nil {
		slog.Error("初始化节点失败", "error", err)
		os.Exit(1)
	}
	defer agent.Close()
	if *retryRejected {
		if err := agent.RetryRejected(); err != nil {
			slog.Error("恢复拒收队列失败", "error", err)
			_ = agent.Close()
			os.Exit(1)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := agent.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("节点执行失败", "error", err)
		os.Exit(1)
	}
}
