package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var files embed.FS

func Open(connectionString string) (*sql.DB, error) {
	database, err := sql.Open("pgx", connectionString)
	if err != nil {
		return nil, fmt.Errorf("open migration database: %w", err)
	}
	return database, nil
}

func Up(ctx context.Context, database *sql.DB) (int, error) {
	root, err := fs.Sub(files, ".")
	if err != nil {
		return 0, fmt.Errorf("migration filesystem: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database, root)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	return len(results), nil
}
