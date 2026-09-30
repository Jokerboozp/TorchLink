package postgres

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var restoredSchemaName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// OpenRestoredReader opens the isolated application artifacts without startup
// migrations, collection baselines, triggers or workers. Additional schemas
// may contain this restore's duty and knowledge artifacts. No public business
// schema fallback or business writes are permitted.
func OpenRestoredReader(ctx context.Context, dsn, applicationSchema string, additionalSchemas ...string) (*Repository, error) {
	if !restoredSchemaName.MatchString(applicationSchema) || !strings.HasPrefix(applicationSchema, "application_restore_") {
		return nil, errors.New("invalid restored application schema")
	}
	schemas := []string{applicationSchema}
	for _, schema := range additionalSchemas {
		if !restoredSchemaName.MatchString(schema) || !(strings.HasPrefix(schema, "duty_restore_") || strings.HasPrefix(schema, "kb_restore_")) {
			return nil, errors.New("invalid restored supporting schema")
		}
		schemas = append(schemas, schema)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("restored reader configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = strings.Join(schemas, ",")
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("restored reader unavailable")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("restored reader unavailable")
	}
	var actual string
	if err = pool.QueryRow(ctx, "SELECT current_schema()").Scan(&actual); err != nil || actual != applicationSchema {
		pool.Close()
		return nil, errors.New("restored reader schema mismatch")
	}
	return &Repository{pool: pool}, nil
}
