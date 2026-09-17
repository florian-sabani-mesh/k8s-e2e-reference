// migrate applies ordered SQL files atomically and rejects changed migration checksums.
package main

import (
	"context"
	"crypto/sha256"
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

func run(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, runtime.Required("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock(710204)"); err != nil {
		return err
	}
	if _, err = conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(runtime.Env("MIGRATIONS_DIR", "migrations"), "*.sql"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no migrations found")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		name := filepath.Base(file)
		sum := fmt.Sprintf("%x", sha256.Sum256(data))
		var previous string
		err = conn.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", name).Scan(&previous)
		if err == nil {
			if previous != sum {
				return fmt.Errorf("migration %s checksum mismatch", name)
			}
			continue
		}
		if err != pgx.ErrNoRows {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(data)); err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", name, sum)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		slog.Info("migration applied", "name", name)
	}
	return nil
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}
