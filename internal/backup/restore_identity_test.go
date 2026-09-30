package backup

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRestoreConnectedIdentityRejectsSourceAliasWithoutDDL(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set IOT_TEST_POSTGRES_DSN for actual restore alias regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("source fixture configuration invalid")
	}
	defer pool.Close()
	alias := dsn
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("fixture source invalid")
		}
		host := u.Hostname()
		if host == "localhost" {
			u.Host = "127.0.0.1:" + u.Port()
		} else if host == "127.0.0.1" {
			u.Host = "localhost:" + u.Port()
		}
		alias = u.String()
	}
	target, err := pgx.Connect(ctx, alias)
	if err != nil {
		t.Fatal("source alias fixture unavailable")
	}
	defer target.Close(context.Background())
	if err = restoreConnectedTargetSafe(ctx, pool, target); !errors.Is(err, ErrRestoreTargetUnsafe) {
		t.Fatal("connected source alias accepted", err)
	}
	// Rechecking also proves the rejected probe released its source lock.
	if err = restoreConnectedTargetSafe(ctx, pool, target); !errors.Is(err, ErrRestoreTargetUnsafe) {
		t.Fatal("source lock cleanup or second probe failed", err)
	}
}
