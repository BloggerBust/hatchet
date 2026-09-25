package main

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sendResult struct {
	MessageID string
}

type emailSender interface {
	send(to string, idempotencyKey string) (sendResult, error)
}

// > stub provider
type emailProvider struct {
	deliveries   []string
	resultsByKey map[string]sendResult
}

func newEmailProvider() *emailProvider {
	return &emailProvider{resultsByKey: map[string]sendResult{}}
}

func (p *emailProvider) send(to string, idempotencyKey string) (sendResult, error) {
	if idempotencyKey != "" {
		if result, ok := p.resultsByKey[idempotencyKey]; ok {
			return result, nil
		}
	}

	p.deliveries = append(p.deliveries, to)
	result := sendResult{MessageID: uuid.NewString()}

	if idempotencyKey != "" {
		p.resultsByKey[idempotencyKey] = result
	}
	return result, nil
}


// > external effect
func sendWelcomeEmail(ctx context.Context, conn *pgx.Conn, provider emailSender, key, to string) (bool, error) {
	var one int
	err := conn.QueryRow(ctx, "SELECT 1 FROM processed_job WHERE idempotency_key = $1", key).Scan(&one)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}

	if _, err := provider.send(to, key); err != nil {
		return false, err
	}

	if _, err := claim(ctx, conn, key); err != nil {
		return false, err
	}
	return true, nil
}

