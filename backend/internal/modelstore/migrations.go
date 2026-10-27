package modelstore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const SchemaVersion = 1

type migration struct {
	version int
	sql     string
}

func builtInMigrations() []migration {
	b, _ := migrationFiles.ReadFile("migrations/001_initial.sql")
	return []migration{{1, string(b)}}
}
func applyMigrations(ctx context.Context, db *sql.DB, ms []migration) error {
	for _, m := range ms {
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, m.sql); e == nil {
			if m.version == 1 {
				for _, table := range []string{"model_revisions", "inference_profiles", "dataset_revisions", "dataset_cases", "environment_snapshots", "reports", "release_policies", "release_events"} {
					for _, op := range []string{"UPDATE", "DELETE"} {
						_, e = tx.ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER %s_no_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'immutable record'); END", table, op, op, table))
						if e != nil {
							break
						}
					}
					if e != nil {
						break
					}
				}
			}
			if e == nil {
				_, e = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(?,?,?)", m.version, now(), scriptChecksum(m.sql))
			}
		}
		if e != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", m.version, e)
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}
func migrationState(ctx context.Context, db *sql.DB) (int, error) {
	var exists int
	if e := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name='schema_migrations'").Scan(&exists); e != nil {
		return 0, e
	}
	if exists == 0 {
		return 0, nil
	}
	rows, e := db.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version")
	if e != nil {
		return 0, e
	}
	defer rows.Close()
	version := 0
	known := builtInMigrations()
	for rows.Next() {
		var v int
		var sum string
		if e = rows.Scan(&v, &sum); e != nil {
			return 0, e
		}
		if v != version+1 {
			return 0, fmt.Errorf("noncontiguous migration history")
		}
		if v <= len(known) && sum != scriptChecksum(known[v-1].sql) {
			return 0, fmt.Errorf("migration %d checksum mismatch", v)
		}
		version = v
	}
	return version, rows.Err()
}

// Git's Windows checkout may use CRLF. Migration identity is based on the
// canonical LF script so the same release does not reject its own database.
func scriptChecksum(script string) string {
	return digest([]byte(strings.ReplaceAll(script, "\r\n", "\n")))
}
