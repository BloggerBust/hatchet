package main

import (
	"context"
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

const schema = `
CREATE TABLE IF NOT EXISTS processed_job (
    idempotency_key text PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS account_credit (
    user_id text NOT NULL,
    amount integer NOT NULL
);
`

func connect(ctx context.Context) (*pgx.Conn, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, errors.New("DATABASE_URL is not set")
	}
	return pgx.Connect(ctx, url)
}
