package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
	"go.uber.org/zap"
)

func newDatabase(path string, memoryLimit int, accessMode DatabaseAccessMode) (*sql.DB, error) {
	dbName := fmt.Sprintf("%s?access_mode=READ_WRITE", path)
	if accessMode == ReadOnlyDatabase {
		dbName = fmt.Sprintf("%s?access_mode=READ_ONLY", path)
	}

	conn, err := sql.Open("duckdb", dbName)
	if err != nil {
		return nil, err
	}

	// DuckDB is single-writer; a single connection prevents idle pool
	// connections from blocking WAL checkpointing.
	conn.SetMaxOpenConns(1)

	// Verify connection works
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Configure extension directory to the same folder as the database
	// This prevents DuckDB from trying to write to ~/.duckdb which may be read-only
	extDir := filepath.Dir(path)
	if _, err := conn.Exec(fmt.Sprintf("SET extension_directory = '%s'", extDir)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("setting extension directory: %w", err)
	}

	if memoryLimit > 0 {
		if _, err := conn.Exec(fmt.Sprintf("SET memory_limit = '%dMB'", memoryLimit)); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("setting memory limit: %w", err)
		}
	}

	zap.S().Infow("connection created to database", "path", path, "memory_limit_MB", memoryLimit, "access_mode", accessMode)

	return conn, nil
}
