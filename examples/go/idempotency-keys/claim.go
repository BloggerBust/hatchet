package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// > atomic claim
func claim(ctx context.Context, db queryRower, key string) (bool, error) {
	var claimed string
	err := db.QueryRow(ctx, `
		INSERT INTO processed_job (idempotency_key)
		VALUES ($1)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key
	`, key).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

