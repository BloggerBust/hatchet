package main

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// > local effect
const signupCredit = 10

func grantSignupCredit(ctx context.Context, tx pgx.Tx, key, userID string) (bool, error) {
	claimed, err := claim(ctx, tx, key)
	if err != nil || !claimed {
		return false, err
	}

	_, err = tx.Exec(ctx, "INSERT INTO account_credit (user_id, amount) VALUES ($1, $2)", userID, signupCredit)
	if err != nil {
		return false, err
	}
	return true, nil
}

// !!
