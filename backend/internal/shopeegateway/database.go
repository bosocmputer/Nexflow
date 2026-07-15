package shopeegateway

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", strings.TrimSpace(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("open gateway database: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping gateway database: %w", err)
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate gateway database: %w", err)
	}
	return db, nil
}

func runMigrations(db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			return fmt.Errorf("execute %s: %w", entry.Name(), err)
		}
	}
	return nil
}
