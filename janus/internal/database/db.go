package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type DB struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("falha ao interpretar DATABASE_URL: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	// Registra os tipos do pgvector no driver pgx
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_ = pgxvec.RegisterTypes(ctx, conn)
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar no PostgreSQL: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping falhou no PostgreSQL: %w", err)
	}

	log.Printf("[DB] Conexão com o PostgreSQL estabelecida com sucesso")
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// HasVectorExtension verifica se a extensão pgvector está ativa no cluster
func (db *DB) HasVectorExtension(ctx context.Context) bool {
	var exists bool
	query := `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`
	_ = db.Pool.QueryRow(ctx, query).Scan(&exists)
	return exists
}

// RunMigrations lê e aplica o schema do 001_init.sql de forma idempotente
func (db *DB) RunMigrations(ctx context.Context, migrationPath string) error {
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("erro ao ler arquivo de migration (%s): %w", migrationPath, err)
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("falha ao iniciar transação de migration: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, string(content)); err != nil {
		return fmt.Errorf("falha ao executar migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("falha ao commitar migration: %w", err)
	}

	log.Printf("[DB] Migrations aplicadas com sucesso: %s", migrationPath)
	return nil
}
