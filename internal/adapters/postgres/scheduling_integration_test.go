package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"                                 /* 执行当前语句并推进处理流程。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"fmt"                                   /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"               /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool"       /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net"                                   /* 执行当前语句并推进处理流程。 */
	"net/url"                               /* 执行当前语句并推进处理流程。 */
	"os"                                    /* 执行当前语句并推进处理流程。 */
	"os/exec"                               /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestSchedulerProcessHelper(t *testing.T) { /* 定义 TestSchedulerProcessHelper 函数。 */
	dsn := os.Getenv("IOT_TEST_SCHEDULER_DSN") /* 更新 dsn 的值。 */
	if dsn == "" {                             /* 判断条件并选择处理分支。 */
		t.Skip("scheduler subprocess only") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                           /* 安排函数结束时执行清理。 */
	r, err := New(ctx, dsn)                                                  /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal("connect scheduler repository") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer r.Close()                                                                          /* 安排函数结束时执行清理。 */
	owner := os.Getenv("IOT_TEST_SCHEDULER_OWNER")                                           /* 更新 owner 的值。 */
	c := protocolruntime.NewCoordinator(r, owner, "http://127.0.0.1:8082")                   /* 更新 c 的值。 */
	reader := protocolruntime.New(r, func(ctx context.Context, raw model.RawMessage) error { /* 更新 reader 的值。 */
		if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		fmt.Println("SCHEDULER_READ", owner) /* 执行当前语句并推进处理流程。 */
		return nil                           /* 返回当前处理结果。 */
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), "127.0.0.0/8") /* 结束当前表达式或代码块。 */
	reader.SetCoordinator(c) /* 执行当前语句并推进处理流程。 */
	reader.Start(ctx)        /* 执行当前语句并推进处理流程。 */
	c.Run(ctx)               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func TestDistributedCollectionProcessFailover(t *testing.T) { /* 定义 TestDistributedCollectionProcessFailover 函数。 */
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN") /* 更新 dsn 的值。 */
	if dsn == "" {                            /* 判断条件并选择处理分支。 */
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                           /* 安排函数结束时执行清理。 */
	admin, err := pgxpool.New(ctx, dsn)                                      /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal("connect test database") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer admin.Close()                                                /* 安排函数结束时执行清理。 */
	schema := fmt.Sprintf("scheduler_%d", time.Now().UnixNano())       /* 更新 schema 的值。 */
	ident := pgx.Identifier{schema}.Sanitize()                         /* 更新 ident 的值。 */
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }() /* 安排函数结束时执行清理。 */
	u, err := url.Parse(dsn)                                                                    /* 更新 err 的值。 */
	if err != nil || !(u.Scheme == "postgres" || u.Scheme == "postgresql") {                    /* 判断条件并选择处理分支。 */
		t.Fatal("test requires a PostgreSQL URL") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	query := u.Query()               /* 更新 query 的值。 */
	query.Set("search_path", schema) /* 执行当前语句并推进处理流程。 */
	u.RawQuery = query.Encode()      /* 更新 u.RawQuery 的值。 */
	testDSN := u.String()            /* 更新 testDSN 的值。 */
	r, err := New(ctx, testDSN)      /* 更新 err 的值。 */
	if err != nil {                  /* 判断条件并选择处理分支。 */
		t.Fatal("initialize isolated repository") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer r.Close()                        /* 安排函数结束时执行清理。 */
	if err := r.Migrate(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	sim, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer sim.Close() /* 安排函数结束时执行清理。 */
	go func() {       /* 执行当前语句并推进处理流程。 */
		for { /* 循环处理当前数据。 */
			conn, err := sim.Accept() /* 更新 err 的值。 */
			if err != nil {           /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			conn.SetDeadline(time.Now().Add(time.Second))         /* 执行当前语句并推进处理流程。 */
			request := make([]byte, 12)                           /* 更新 request 的值。 */
			if _, err := io.ReadFull(conn, request); err == nil { /* 判断条件并选择处理分支。 */
				conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42}) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			conn.Close() /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "modbus", Version: "1", Transport: "MODBUS_TCP", Status: "PUBLISHED", Config: map[string]any{"blocks": []model.ModbusReadBlock{{ID: "read", FunctionCode: 3, StartAddress: 0, Quantity: 1, PollIntervalSec: 1}}}} /* 更新 release 的值。 */
	if err := r.CreateProtocolRelease(ctx, release); err != nil {                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", ProductID: "product", DeviceID: "device", ProtocolID: "modbus", ProtocolVersion: "1", Mode: "poll", Host: "127.0.0.1", Port: sim.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500, Enabled: true} /* 更新 profile 的值。 */
	if err := r.SaveDeviceAccessProfile(ctx, profile); err != nil {                                                                                                                                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	executable, err := os.Executable() /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	reads := make(chan string, 64)          /* 更新 reads 的值。 */
	start := func(owner string) *exec.Cmd { /* 更新 start 的值。 */
		command := exec.CommandContext(ctx, executable, "-test.run=^TestSchedulerProcessHelper$", "-test.v")     /* 更新 command 的值。 */
		command.Env = append(os.Environ(), "IOT_TEST_SCHEDULER_DSN="+testDSN, "IOT_TEST_SCHEDULER_OWNER="+owner) /* 更新 command.Env 的值。 */
		stdout, err := command.StdoutPipe()                                                                      /* 更新 err 的值。 */
		if err != nil {                                                                                          /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		command.Stderr = io.Discard             /* 更新 command.Stderr 的值。 */
		if err := command.Start(); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			scanner := bufio.NewScanner(stdout) /* 更新 scanner 的值。 */
			for scanner.Scan() {                /* 循环处理当前数据。 */
				if strings.HasPrefix(scanner.Text(), "SCHEDULER_READ ") { /* 判断条件并选择处理分支。 */
					select { /* 根据条件选择处理路径。 */
					case reads <- strings.TrimPrefix(scanner.Text(), "SCHEDULER_READ "): /* 处理当前分支。 */
					case <-ctx.Done(): /* 处理当前分支。 */
						return /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
		return command /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	first := start("first")                               /* 更新 first 的值。 */
	defer func() { first.Process.Kill(); first.Wait() }() /* 安排函数结束时执行清理。 */
	select {                                              /* 根据条件选择处理路径。 */
	case owner := <-reads: /* 处理当前分支。 */
		if owner != "first" { /* 判断条件并选择处理分支。 */
			t.Fatal(owner) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		t.Fatal("first scheduler did not collect") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	lease, err := r.GetExecutionLease(ctx, "tenant", "profile/profile") /* 更新 err 的值。 */
	if err != nil || lease.Owner != "first" {                           /* 判断条件并选择处理分支。 */
		t.Fatal("first lease", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second := start("second")                               /* 更新 second 的值。 */
	defer func() { second.Process.Kill(); second.Wait() }() /* 安排函数结束时执行清理。 */
	for i := 0; i < 2; i++ {                                /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case owner := <-reads: /* 处理当前分支。 */
			if owner != "first" { /* 判断条件并选择处理分支。 */
				t.Fatal("duplicate active scheduler", owner) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			t.Fatal("collection stopped before failure") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := first.Process.Kill(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for { /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case owner := <-reads: /* 处理当前分支。 */
			if owner != "second" { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			next, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")     /* 更新 err 的值。 */
			if err != nil || next.Owner != "second" || next.Token <= lease.Token { /* 判断条件并选择处理分支。 */
				t.Fatal("takeover did not fence old token", next, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := r.ReleaseExecutionLease(ctx, lease); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			current, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")                       /* 更新 err 的值。 */
			if err != nil || current.Owner != "second" || current.ExpiresAt <= time.Now().UnixMilli() { /* 判断条件并选择处理分支。 */
				t.Fatal("old owner released active successor", current, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			t.Fatal("surviving scheduler did not take over after process loss") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
