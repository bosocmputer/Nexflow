package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"strings"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationAdvisoryLockKey is stable across Nexflow containers that point at
// the same tenant database. A session-level lock is used because every SQL
// migration is intentionally replayable and multiple containers may start at
// the same time during a rollout.
const migrationAdvisoryLockKey int64 = 0x4e4558464c4f57 // "NEXFLOW"

func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	log.Println("database connected and migrated")
	return db, nil
}

func runMigrations(db *sql.DB) error {
	return withMigrationLock(db, func(conn *sql.Conn) error {
		entries, err := migrationFS.ReadDir("migrations")
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			data, err := migrationFS.ReadFile("migrations/" + entry.Name())
			if err != nil {
				return fmt.Errorf("read %s: %w", entry.Name(), err)
			}
			if _, err := conn.ExecContext(context.Background(), string(data)); err != nil {
				return fmt.Errorf("exec %s: %w", entry.Name(), err)
			}
			log.Printf("migration applied: %s", entry.Name())
		}
		return nil
	})
}

func withMigrationLock(db *sql.DB, migrate func(*sql.Conn) error) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}

	migrateErr := migrate(conn)
	_, unlockErr := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("release migration advisory lock: %w", unlockErr)
	}
	return errors.Join(migrateErr, unlockErr)
}
