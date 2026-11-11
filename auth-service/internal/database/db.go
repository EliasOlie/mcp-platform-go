package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("falha ao interpretar DATABASE_URL: %w", err)
	}

	config.MaxConns = 30
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar no PostgreSQL do Auth: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping falhou no banco do Auth: %w", err)
	}

	log.Printf("[Auth-DB] Conexão com o banco de dados estabelecida com sucesso")
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

func (db *DB) RunMigrations(ctx context.Context, migrationPath string) error {
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("erro ao ler migration (%s): %w", migrationPath, err)
	}

	_, err = db.Pool.Exec(ctx, string(content))
	if err != nil {
		return fmt.Errorf("falha ao executar migration do Auth: %w", err)
	}

	log.Printf("[Auth-DB] Migrations aplicadas com sucesso: %s", migrationPath)
	return nil
}
