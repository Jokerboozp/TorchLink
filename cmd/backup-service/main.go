package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"iot-platform/internal/backup"
	"iot-platform/internal/config"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	envFile := flag.String("env-file", "", "load a KEY=VALUE configuration file (existing environment variables take precedence)")
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			log.Error("load environment file", "error", err)
			os.Exit(1)
		}
	}
	service, err := backup.New(ctx, backup.Config{
		PostgresDSN: os.Getenv("IOT_POSTGRES_DSN"), BackupDir: env("IOT_BACKUP_DIR", "./data/backups"), BackupBucket: env("IOT_BACKUP_BUCKET", "iot-backups"),
		MinIOEndpoint: os.Getenv("IOT_MINIO_ENDPOINT"), MinIOAccessKey: os.Getenv("IOT_MINIO_ACCESS_KEY"), MinIOSecretKey: os.Getenv("IOT_MINIO_SECRET_KEY"), MinIOUseTLS: boolean("IOT_MINIO_USE_TLS"),
		ClickHouseURL:  os.Getenv("IOT_CLICKHOUSE_URL"),
		BackupTimezone: env("IOT_BACKUP_TIMEZONE", "Asia/Shanghai"),
	})
	if err != nil {
		log.Error("initialize backup service", "error", err)
		os.Exit(1)
	}
	defer service.Close()
	if boolean("IOT_BACKUP_ONCE") {
		kind := env("IOT_BACKUP_TYPE", "FULL")
		result, runErr := service.Run(ctx, kind)
		if runErr != nil {
			log.Error("one-shot backup failed", "type", kind, "error", runErr)
			os.Exit(1)
		}
		log.Info("one-shot backup completed", "type", kind, "id", result.ID)
		return
	}
	adminToken := env("IOT_BACKUP_ADMIN_TOKEN", "change-me-backup-admin-token")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, readyCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer readyCancel()
		if readyErr := service.Ready(readyCtx); readyErr != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "DOWN", "error": readyErr.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(service.Metrics()))
	})
	mux.HandleFunc("POST /backup", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		result, runErr := service.Run(r.Context(), r.URL.Query().Get("type"))
		respond(w, result, runErr)
	}))
	mux.HandleFunc("POST /restore/drill", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		result, runErr := service.Verify(r.Context(), r.URL.Query().Get("backupId"))
		respond(w, result, runErr)
	}))
	mux.HandleFunc("GET /backups", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		result, listErr := service.ListTasks(r.Context(), query.Get("type"), query.Get("status"), intQuery(query.Get("limit"), 50), intQuery(query.Get("offset"), 0))
		respond(w, result, listErr)
	}))
	mux.HandleFunc("GET /backups/{id}", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		result, getErr := service.GetTask(r.Context(), r.PathValue("id"))
		respond(w, result, getErr)
	}))
	mux.HandleFunc("GET /backups/{id}/files", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		limit := intQuery(query.Get("pageSize"), intQuery(query.Get("limit"), 20))
		offset := intQuery(query.Get("offset"), 0)
		if page := intQuery(query.Get("page"), 0); page > 0 {
			offset = (page - 1) * limit
		}
		result, listErr := service.ListArtifactsPage(r.Context(), r.PathValue("id"), limit, offset)
		respond(w, result, listErr)
	}))
	mux.HandleFunc("GET /backups/{id}/files/{filename}", protected(adminToken, func(w http.ResponseWriter, r *http.Request) {
		object, artifact, openErr := service.OpenArtifact(r.Context(), r.PathValue("id"), r.PathValue("filename"))
		if openErr != nil {
			respond(w, nil, openErr)
			return
		}
		defer object.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, artifact.Filename))
		w.Header().Set("X-Checksum-SHA256", artifact.SHA256)
		if artifact.Size >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(artifact.Size, 10))
		}
		if _, copyErr := io.Copy(w, object); copyErr != nil {
			log.Error("backup artifact download failed", "backupId", r.PathValue("id"), "filename", artifact.Filename, "error", copyErr)
		}
	}))
	server := &http.Server{Addr: env("IOT_BACKUP_HTTP_ADDR", ":8090"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if listenErr := server.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
			log.Error("backup HTTP service failed", "error", listenErr)
			cancel()
		}
	}()
	if !strings.EqualFold(env("IOT_BACKUP_ENABLED", "true"), "false") {
		go dailyDeviceDataScheduler(ctx, service, log, env("IOT_BACKUP_TIME", "00:05"), env("IOT_BACKUP_TIMEZONE", "Asia/Shanghai"))
	}
	<-ctx.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
}

func dailyDeviceDataScheduler(ctx context.Context, service *backup.Service, log *slog.Logger, clock, timezone string) {
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		log.Warn("invalid backup timezone; using UTC", "timezone", timezone, "error", err)
		location = time.UTC
	}
	for {
		next, parseErr := nextDailyRun(time.Now().In(location), clock, location)
		if parseErr != nil {
			log.Error("invalid daily device-data backup time", "time", clock, "error", parseErr)
			clock = "00:05"
			continue
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			backupDay := next.AddDate(0, 0, -1)
			result, runErr := service.RunDaily(ctx, backupDay)
			if runErr != nil {
				log.Error("scheduled device-data backup failed", "type", "DEVICE_DAILY", "date", backupDay.Format("2006-01-02"), "error", runErr)
			} else {
				log.Info("scheduled device-data backup completed", "type", "DEVICE_DAILY", "date", backupDay.Format("2006-01-02"), "id", result.ID)
			}
		}
	}
}

func nextDailyRun(now time.Time, clock string, location *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation("15:04", strings.TrimSpace(clock), location)
	if err != nil {
		return time.Time{}, err
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}
func protected(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
func respond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func intQuery(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func boolean(name string) bool {
	value, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(name)))
	return value
}
