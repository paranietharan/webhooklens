package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"webhooklens/internal/config"
)

func main() {
	cfg := config.Load()

	dirFlag := flag.String("path", "", "path to migrations directory")
	dbFlag := flag.String("database", "", "database connection URL")
	flag.Parse()

	dbURL := cfg.DatabaseURL
	if *dbFlag != "" {
		dbURL = *dbFlag
	}

	command := "up"
	if len(flag.Args()) > 0 {
		command = strings.ToLower(flag.Args()[0])
	}

	migrationsDir, err := resolveMigrationsDir(*dirFlag)
	if err != nil {
		log.Fatalf("[migration] fatal: %v", err)
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("[migration] fatal: failed to connect to database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("[migration] fatal: failed to ping database: %v", err)
	}

	if err := initSchemaTable(ctx, db); err != nil {
		log.Fatalf("[migration] fatal: failed to initialize schema_migrations: %v", err)
	}

	switch command {
	case "up":
		if err := runUp(ctx, db, migrationsDir); err != nil {
			log.Fatalf("[migration] failed running up migrations: %v", err)
		}
	case "down":
		if err := runDown(ctx, db, migrationsDir); err != nil {
			log.Fatalf("[migration] failed running down migration: %v", err)
		}
	case "status":
		if err := printStatus(ctx, db, migrationsDir); err != nil {
			log.Fatalf("[migration] failed retrieving status: %v", err)
		}
	default:
		log.Fatalf("[migration] unknown command %q, use 'up', 'down', or 'status'", command)
	}
}

func resolveMigrationsDir(explicitPath string) (string, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath, nil
		}
		return "", fmt.Errorf("specified migration path %q does not exist", explicitPath)
	}

	candidates := []string{
		"migrations",
		"../migrations",
		"../../migrations",
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c, nil
		}
	}

	return "", fmt.Errorf("could not find 'migrations' directory in standard search paths")
}

func initSchemaTable(ctx context.Context, db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func getAppliedMigrations(ctx context.Context, db *sql.DB) (map[string]time.Time, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, applied_at FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]time.Time)
	for rows.Next() {
		var version string
		var appliedAt time.Time
		if err := rows.Scan(&version, &appliedAt); err != nil {
			return nil, err
		}
		applied[version] = appliedAt
	}
	return applied, rows.Err()
}

func runUp(ctx context.Context, db *sql.DB, migrationsDir string) error {
	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	appliedCount := 0
	for _, file := range upFiles {
		version := strings.TrimSuffix(file, ".up.sql")
		if _, alreadyApplied := applied[version]; alreadyApplied {
			continue
		}

		filePath := filepath.Join(migrationsDir, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed reading %s: %w", file, err)
		}

		log.Printf("[migration] applying %s...", file)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed executing %s: %w", file, err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed recording migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed committing transaction for %s: %w", file, err)
		}

		log.Printf("[migration] applied %s successfully", file)
		appliedCount++
	}

	if appliedCount == 0 {
		log.Println("[migration] database schema is already up to date")
	} else {
		log.Printf("[migration] finished applying %d migration(s)", appliedCount)
	}

	return nil
}

func runDown(ctx context.Context, db *sql.DB, migrationsDir string) error {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
	if err != nil {
		return err
	}
	defer rows.Close()

	var lastVersion string
	if rows.Next() {
		if err := rows.Scan(&lastVersion); err != nil {
			return err
		}
	} else {
		log.Println("[migration] no migrations to rollback")
		return nil
	}

	downFile := lastVersion + ".down.sql"
	filePath := filepath.Join(migrationsDir, downFile)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed reading down file %s: %w", downFile, err)
	}

	log.Printf("[migration] rolling back %s...", downFile)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed executing rollback %s: %w", downFile, err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = $1", lastVersion); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed deleting record %s: %w", lastVersion, err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("[migration] successfully rolled back %s", lastVersion)
	return nil
}

func printStatus(ctx context.Context, db *sql.DB, migrationsDir string) error {
	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	fmt.Printf("%-35s %-12s %s\n", "MIGRATION", "STATUS", "APPLIED AT")
	fmt.Println(strings.Repeat("-", 65))
	for _, file := range upFiles {
		version := strings.TrimSuffix(file, ".up.sql")
		appliedAt, isApplied := applied[version]
		status := "PENDING"
		dateStr := "-"
		if isApplied {
			status = "APPLIED"
			dateStr = appliedAt.Format(time.RFC3339)
		}
		fmt.Printf("%-35s %-12s %s\n", version, status, dateStr)
	}
	return nil
}
