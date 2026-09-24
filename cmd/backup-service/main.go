package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"
	"flag" /* 执行当前语句并推进处理流程。 */
	"fmt"  /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"
	"io"        /* 执行当前语句并推进处理流程。 */
	"log/slog"  /* 执行当前语句并推进处理流程。 */
	"net/http"  /* 执行当前语句并推进处理流程。 */
	"os"        /* 执行当前语句并推进处理流程。 */
	"os/signal" /* 执行当前语句并推进处理流程。 */
	"strconv"   /* 执行当前语句并推进处理流程。 */
	"strings"   /* 执行当前语句并推进处理流程。 */
	"syscall"   /* 执行当前语句并推进处理流程。 */
	"time"      /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/backup" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)                                       /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                 /* 安排函数结束时执行清理。 */
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))                                                                           /* 更新 log 的值。 */
	envFile := flag.String("env-file", "", "load a KEY=VALUE configuration file (existing environment variables take precedence)") /* 更新 envFile 的值。 */
	flag.Parse()                                                                                                                   /* 执行当前语句并推进处理流程。 */
	if *envFile != "" {                                                                                                            /* 判断条件并选择处理分支。 */
		if err := config.LoadEnvFile(*envFile); err != nil { /* 判断条件并选择处理分支。 */
			log.Error("load environment file", "error", err) /* 执行当前语句并推进处理流程。 */
			os.Exit(1)                                       /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	service, err := backup.New(ctx, backup.Config{ /* 更新 err 的值。 */
		PostgresDSN: os.Getenv("IOT_POSTGRES_DSN"), BackupDir: env("IOT_BACKUP_DIR", "./data/backups"), BackupBucket: env("IOT_BACKUP_BUCKET", "iot-backups"), /* 执行当前语句并推进处理流程。 */
		MinIOEndpoint: os.Getenv("IOT_MINIO_ENDPOINT"), MinIOAccessKey: os.Getenv("IOT_MINIO_ACCESS_KEY"), MinIOSecretKey: os.Getenv("IOT_MINIO_SECRET_KEY"), MinIOUseTLS: boolean("IOT_MINIO_USE_TLS"), /* 执行当前语句并推进处理流程。 */
		ClickHouseURL:  os.Getenv("IOT_CLICKHOUSE_URL"),             /* 执行当前语句并推进处理流程。 */
		BackupTimezone: env("IOT_BACKUP_TIMEZONE", "Asia/Shanghai"), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		log.Error("initialize backup service", "error", err) /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                                           /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	defer service.Close()           /* 安排函数结束时执行清理。 */
	if boolean("IOT_BACKUP_ONCE") { /* 判断条件并选择处理分支。 */
		kind := env("IOT_BACKUP_TYPE", "FULL")   /* 更新 kind 的值。 */
		result, runErr := service.Run(ctx, kind) /* 更新 runErr 的值。 */
		if runErr != nil {                       /* 判断条件并选择处理分支。 */
			log.Error("one-shot backup failed", "type", kind, "error", runErr) /* 执行当前语句并推进处理流程。 */
			os.Exit(1)                                                         /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		log.Info("one-shot backup completed", "type", kind, "id", result.ID) /* 执行当前语句并推进处理流程。 */
		return                                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	adminToken := env("IOT_BACKUP_ADMIN_TOKEN", "change-me-backup-admin-token")       /* 更新 adminToken 的值。 */
	mux := http.NewServeMux()                                                         /* 更新 mux 的值。 */
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { /* 执行当前语句并推进处理流程。 */
		json.NewEncoder(w).Encode(map[string]string{"status": "UP"}) /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		readyCtx, readyCancel := context.WithTimeout(r.Context(), 5*time.Second) /* 更新 readyCancel 的值。 */
		defer readyCancel()                                                      /* 安排函数结束时执行清理。 */
		if readyErr := service.Ready(readyCtx); readyErr != nil {                /* 判断条件并选择处理分支。 */
			w.Header().Set("Content-Type", "application/json")                                            /* 执行当前语句并推进处理流程。 */
			w.WriteHeader(http.StatusServiceUnavailable)                                                  /* 执行当前语句并推进处理流程。 */
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "DOWN", "error": readyErr.Error()}) /* 更新 _ 的值。 */
			return                                                                                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "UP"}) /* 更新 _ 的值。 */
	}) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) { /* 执行当前语句并推进处理流程。 */
		w.Header().Set("Content-Type", "text/plain; version=0.0.4") /* 执行当前语句并推进处理流程。 */
		_, _ = w.Write([]byte(service.Metrics()))                   /* 更新 _ 的值。 */
	}) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("POST /backup", protected(adminToken, func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		result, runErr := service.Run(r.Context(), r.URL.Query().Get("type")) /* 更新 runErr 的值。 */
		respond(w, result, runErr)                                            /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("POST /restore/drill", protected(adminToken, func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		result, runErr := service.Verify(r.Context(), r.URL.Query().Get("backupId")) /* 更新 runErr 的值。 */
		respond(w, result, runErr)                                                   /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("GET /backups", protected(adminToken, func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		query := r.URL.Query()                                                                                                                                        /* 更新 query 的值。 */
		result, listErr := service.ListTasks(r.Context(), query.Get("type"), query.Get("status"), intQuery(query.Get("limit"), 50), intQuery(query.Get("offset"), 0)) /* 更新 listErr 的值。 */
		respond(w, result, listErr)                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("GET /backups/{id}", protected(adminToken, func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		result, getErr := service.GetTask(r.Context(), r.PathValue("id")) /* 更新 getErr 的值。 */
		respond(w, result, getErr)                                        /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("DELETE /backups/{id}", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		err := service.DeleteTask(r.Context(), r.PathValue("id"))
		status := http.StatusOK
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			status = http.StatusNotFound
		case errors.Is(err, backup.ErrTaskRunning), errors.Is(err, backup.ErrTaskReferenced):
			status = http.StatusConflict
		case err != nil:
			status = http.StatusInternalServerError
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		}
	}))
	mux.HandleFunc("GET /backups/{id}/files", protected(adminToken, func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		query := r.URL.Query()                                                     /* 更新 query 的值。 */
		limit := intQuery(query.Get("pageSize"), intQuery(query.Get("limit"), 20)) /* 更新 limit 的值。 */
		offset := intQuery(query.Get("offset"), 0)                                 /* 更新 offset 的值。 */
		if page := intQuery(query.Get("page"), 0); page > 0 {                      /* 判断条件并选择处理分支。 */
			offset = (page - 1) * limit /* 更新 offset 的值。 */
		} /* 结束当前表达式或代码块。 */
		result, listErr := service.ListArtifactsPage(r.Context(), r.PathValue("id"), limit, offset) /* 更新 listErr 的值。 */
		respond(w, result, listErr)                                                                 /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	mux.HandleFunc("GET /backups/{id}/files/{filename}", protected(adminToken, func(w http.ResponseWriter, r *http.Request) { /* 执行当前语句并推进处理流程。 */
		object, artifact, openErr := service.OpenArtifact(r.Context(), r.PathValue("id"), r.PathValue("filename")) /* 更新 openErr 的值。 */
		if openErr != nil {                                                                                        /* 判断条件并选择处理分支。 */
			respond(w, nil, openErr) /* 执行当前语句并推进处理流程。 */
			return                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		defer object.Close()                                                                               /* 安排函数结束时执行清理。 */
		w.Header().Set("Content-Type", "application/octet-stream")                                         /* 执行当前语句并推进处理流程。 */
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, artifact.Filename)) /* 执行当前语句并推进处理流程。 */
		w.Header().Set("X-Checksum-SHA256", artifact.SHA256)                                               /* 执行当前语句并推进处理流程。 */
		if artifact.Size >= 0 {                                                                            /* 判断条件并选择处理分支。 */
			w.Header().Set("Content-Length", strconv.FormatInt(artifact.Size, 10)) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, copyErr := io.Copy(w, object); copyErr != nil { /* 判断条件并选择处理分支。 */
			log.Error("backup artifact download failed", "backupId", r.PathValue("id"), "filename", artifact.Filename, "error", copyErr) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	server := &http.Server{Addr: env("IOT_BACKUP_HTTP_ADDR", ":8090"), Handler: mux, ReadHeaderTimeout: 5 * time.Second} /* 更新 server 的值。 */
	go func() {                                                                                                          /* 执行当前语句并推进处理流程。 */
		if listenErr := server.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed { /* 判断条件并选择处理分支。 */
			log.Error("backup HTTP service failed", "error", listenErr) /* 执行当前语句并推进处理流程。 */
			cancel()                                                    /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	if !strings.EqualFold(env("IOT_BACKUP_ENABLED", "true"), "false") { /* 判断条件并选择处理分支。 */
		go dailyDeviceDataScheduler(ctx, service, log, env("IOT_BACKUP_TIME", "00:05"), env("IOT_BACKUP_TIMEZONE", "Asia/Shanghai")) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	<-ctx.Done()                                                                /* 执行当前语句并推进处理流程。 */
	shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second) /* 更新 stop 的值。 */
	defer stop()                                                                /* 安排函数结束时执行清理。 */
	_ = server.Shutdown(shutdown)                                               /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */

func dailyDeviceDataScheduler(ctx context.Context, service *backup.Service, log *slog.Logger, clock, timezone string) { /* 定义 dailyDeviceDataScheduler 函数。 */
	location, err := time.LoadLocation(strings.TrimSpace(timezone)) /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		log.Warn("invalid backup timezone; using UTC", "timezone", timezone, "error", err) /* 执行当前语句并推进处理流程。 */
		location = time.UTC                                                                /* 更新 location 的值。 */
	} /* 结束当前表达式或代码块。 */
	for { /* 循环处理当前数据。 */
		next, parseErr := nextDailyRun(time.Now().In(location), clock, location) /* 更新 parseErr 的值。 */
		if parseErr != nil {                                                     /* 判断条件并选择处理分支。 */
			log.Error("invalid daily device-data backup time", "time", clock, "error", parseErr) /* 执行当前语句并推进处理流程。 */
			clock = "00:05"                                                                      /* 更新 clock 的值。 */
			continue                                                                             /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		timer := time.NewTimer(time.Until(next)) /* 更新 timer 的值。 */
		select {                                 /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			if !timer.Stop() { /* 判断条件并选择处理分支。 */
				select { /* 根据条件选择处理路径。 */
				case <-timer.C: /* 处理当前分支。 */
				default: /* 处理当前分支。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		case <-timer.C: /* 处理当前分支。 */
			backupDay := next.AddDate(0, 0, -1)                /* 更新 backupDay 的值。 */
			result, runErr := service.RunDaily(ctx, backupDay) /* 更新 runErr 的值。 */
			if runErr != nil {                                 /* 判断条件并选择处理分支。 */
				log.Error("scheduled device-data backup failed", "type", "DEVICE_DAILY", "date", backupDay.Format("2006-01-02"), "error", runErr) /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				log.Info("scheduled device-data backup completed", "type", "DEVICE_DAILY", "date", backupDay.Format("2006-01-02"), "id", result.ID) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func nextDailyRun(now time.Time, clock string, location *time.Location) (time.Time, error) { /* 定义 nextDailyRun 函数。 */
	parsed, err := time.ParseInLocation("15:04", strings.TrimSpace(clock), location) /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		return time.Time{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	next := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location) /* 更新 next 的值。 */
	if !next.After(now) {                                                                                 /* 判断条件并选择处理分支。 */
		next = next.AddDate(0, 0, 1) /* 更新 next 的值。 */
	} /* 结束当前表达式或代码块。 */
	return next, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func protected(token string, next http.HandlerFunc) http.HandlerFunc { /* 定义 protected 函数。 */
	return func(w http.ResponseWriter, r *http.Request) { /* 返回当前处理结果。 */
		if r.Header.Get("Authorization") != "Bearer "+token { /* 判断条件并选择处理分支。 */
			http.Error(w, "unauthorized", http.StatusUnauthorized) /* 执行当前语句并推进处理流程。 */
			return                                                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		next(w, r) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func respond(w http.ResponseWriter, value any, err error) { /* 定义 respond 函数。 */
	w.Header().Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		w.WriteHeader(http.StatusInternalServerError)                          /* 执行当前语句并推进处理流程。 */
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) /* 更新 _ 的值。 */
		return                                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.NewEncoder(w).Encode(value) /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */
func env(name, fallback string) string { /* 定义 env 函数。 */
	if value := os.Getenv(name); value != "" { /* 判断条件并选择处理分支。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func intQuery(value string, fallback int) int { /* 定义 intQuery 函数。 */
	parsed, err := strconv.Atoi(strings.TrimSpace(value)) /* 更新 err 的值。 */
	if err != nil {                                       /* 判断条件并选择处理分支。 */
		return fallback /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return parsed /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func boolean(name string) bool { /* 定义 boolean 函数。 */
	value, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(name))) /* 更新 _ 的值。 */
	return value                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
