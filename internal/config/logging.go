package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// LogLevel reads IOT_LOG_LEVEL. The default info level keeps routine
// successful HTTP requests out of the logs; debug records every request.
func LogLevel() (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("IOT_LOG_LEVEL"))) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("IOT_LOG_LEVEL must be debug, info, warn or error")
	}
}
