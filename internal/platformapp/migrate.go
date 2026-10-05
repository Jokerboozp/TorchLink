package platformapp

import (
	"context"
	"fmt"
	"os"
	"time"

	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/config"
)

// migrateCommand runs "iot-platform migrate [--check]": apply the database
// migrations and exit, or with --check only list what an upgrade would apply.
// Starting the platform still migrates on its own; this lets an upgrade
// migrate (and see how much) before replacing the running processes.
func migrateCommand(args []string) int {
	check := len(args) == 1 && args[0] == "--check"
	if len(args) > 1 || len(args) == 1 && !check {
		fmt.Fprintln(os.Stderr, "usage: iot-platform [--env-file FILE] migrate [--check]")
		return 2
	}
	dsn := config.Load().PostgresDSN
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "IOT_POSTGRES_DSN is not set")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	pending, err := postgres.Pending(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read migrations:", err)
		return 1
	}
	for _, name := range pending {
		fmt.Println(name)
	}
	if check {
		fmt.Fprintf(os.Stderr, "%d pending migration(s)\n", len(pending))
		return 0
	}
	if err = postgres.MigrateDSN(ctx, dsn); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "applied %d migration(s)\n", len(pending))
	return 0
}
