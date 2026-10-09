// Package db opens the Postgres pool and applies the schema.
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schema string

// Arbitrary constant shared by all replicas of this service.
const migrationLockID = 727_001

func Open(ctx context.Context, url string) (*sql.DB, error) {
	pool, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(20)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(30 * time.Minute)

	// Postgres may still be starting (compose, k8s); retry for a while.
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = pool.PingContext(ctx)
		if err == nil || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres not reachable: %w", err)
	}
	return pool, nil
}

// Migrate applies schema.sql under a session-level advisory lock.
func Migrate(ctx context.Context, pool *sql.DB) error {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
