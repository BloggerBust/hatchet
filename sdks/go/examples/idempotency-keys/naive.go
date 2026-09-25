package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// > check then insert
func grantSignupCreditNaive(ctx context.Context, conn *pgx.Conn, key, userID string) (bool, error) {
	var one int
	err := conn.QueryRow(ctx, "SELECT 1 FROM processed_job WHERE idempotency_key = $1", key).Scan(&one)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}

	if _, err := conn.Exec(ctx, "INSERT INTO account_credit (user_id, amount) VALUES ($1, $2)", userID, signupCredit); err != nil {
		return false, err
	}

	if _, err := conn.Exec(ctx, "INSERT INTO processed_job (idempotency_key) VALUES ($1)", key); err != nil {
		return false, err
	}

	return true, nil
}

// !!
