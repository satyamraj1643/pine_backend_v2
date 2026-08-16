package db

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("Unable to parse DATABASE_URL: %v", err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	// Supabase free tier allows ~60 direct connections; keep pool conservative
	config.MaxConns = 10
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}

	Pool = pool
	log.Println("Database connected to Supabase (pool)")
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
