package postgres

import (
	"bufio"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolruntime"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSchedulerProcessHelper(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_SCHEDULER_DSN")
	if dsn == "" {
		t.Skip("scheduler subprocess only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Fatal("connect scheduler repository")
	}
	defer r.Close()
	owner := os.Getenv("IOT_TEST_SCHEDULER_OWNER")
	c := protocolruntime.NewCoordinator(r, owner, "http://127.0.0.1:8082")
	reader := protocolruntime.New(r, func(ctx context.Context, raw model.RawMessage) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Println("SCHEDULER_READ", owner)
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), "127.0.0.0/8")
	reader.SetCoordinator(c)
	reader.Start(ctx)
	c.Run(ctx)
}

func TestDistributedCollectionProcessFailover(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("connect test database")
	}
	defer admin.Close()
	schema := fmt.Sprintf("scheduler_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }()
	u, err := url.Parse(dsn)
	if err != nil || !(u.Scheme == "postgres" || u.Scheme == "postgresql") {
		t.Fatal("test requires a PostgreSQL URL")
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	testDSN := u.String()
	r, err := New(ctx, testDSN)
	if err != nil {
		t.Fatal("initialize isolated repository")
	}
	defer r.Close()
	if err := r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	sim, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	go func() {
		for {
			conn, err := sim.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(time.Second))
			request := make([]byte, 12)
			if _, err := io.ReadFull(conn, request); err == nil {
				conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42})
			}
			conn.Close()
		}
	}()
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "modbus", Version: "1", Transport: "MODBUS_TCP", Status: "PUBLISHED", Config: map[string]any{"blocks": []model.ModbusReadBlock{{ID: "read", FunctionCode: 3, StartAddress: 0, Quantity: 1, PollIntervalSec: 1}}}}
	if err := r.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", ProductID: "product", DeviceID: "device", ProtocolID: "modbus", ProtocolVersion: "1", Mode: "poll", Host: "127.0.0.1", Port: sim.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500, Enabled: true}
	if err := r.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reads := make(chan string, 64)
	start := func(owner string) *exec.Cmd {
		command := exec.CommandContext(ctx, executable, "-test.run=^TestSchedulerProcessHelper$", "-test.v")
		command.Env = append(os.Environ(), "IOT_TEST_SCHEDULER_DSN="+testDSN, "IOT_TEST_SCHEDULER_OWNER="+owner)
		stdout, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "SCHEDULER_READ ") {
					select {
					case reads <- strings.TrimPrefix(scanner.Text(), "SCHEDULER_READ "):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return command
	}
	first := start("first")
	defer func() { first.Process.Kill(); first.Wait() }()
	select {
	case owner := <-reads:
		if owner != "first" {
			t.Fatal(owner)
		}
	case <-ctx.Done():
		t.Fatal("first scheduler did not collect")
	}
	lease, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")
	if err != nil || lease.Owner != "first" {
		t.Fatal("first lease", err)
	}
	second := start("second")
	defer func() { second.Process.Kill(); second.Wait() }()
	for i := 0; i < 2; i++ {
		select {
		case owner := <-reads:
			if owner != "first" {
				t.Fatal("duplicate active scheduler", owner)
			}
		case <-ctx.Done():
			t.Fatal("collection stopped before failure")
		}
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case owner := <-reads:
			if owner != "second" {
				continue
			}
			next, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")
			if err != nil || next.Owner != "second" || next.Token <= lease.Token {
				t.Fatal("takeover did not fence old token", next, err)
			}
			if err := r.ReleaseExecutionLease(ctx, lease); err != nil {
				t.Fatal(err)
			}
			current, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")
			if err != nil || current.Owner != "second" || current.ExpiresAt <= time.Now().UnixMilli() {
				t.Fatal("old owner released active successor", current, err)
			}
			return
		case <-ctx.Done():
			t.Fatal("surviving scheduler did not take over after process loss")
		}
	}
}
