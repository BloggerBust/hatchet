package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errLostResponse = errors.New("provider response lost")

type losesFirstResponse struct {
	inner emailSender
	lost  bool
}

func (s *losesFirstResponse) send(to string, idempotencyKey string) (sendResult, error) {
	result, err := s.inner.send(to, idempotencyKey)
	if err != nil {
		return result, err
	}
	if !s.lost {
		s.lost = true
		return sendResult{}, errLostResponse
	}
	return result, nil
}

type ignoresIdempotencyKey struct {
	inner emailSender
}

func (s *ignoresIdempotencyKey) send(to string, _ string) (sendResult, error) {
	return s.inner.send(to, "")
}

func attempt(ctx context.Context, conn *pgx.Conn, sender emailSender, provider *emailProvider, key, to string) (string, error) {
	var outcome string
	called, err := sendWelcomeEmail(ctx, conn, sender, key, to)
	switch {
	case errors.Is(err, errLostResponse):
		outcome = "response lost"
	case err != nil:
		return "", err
	case called:
		outcome = "called the provider"
	default:
		outcome = "already recorded"
	}

	var one int
	err = conn.QueryRow(ctx, "SELECT 1 FROM processed_job WHERE idempotency_key = $1", key).Scan(&one)
	record := "present"
	if errors.Is(err, pgx.ErrNoRows) {
		record = "absent"
	} else if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s (record: %s, deliveries: %d)", outcome, record, len(provider.deliveries)), nil
}

func lostRunner(ctx context.Context, conn *pgx.Conn, sender emailSender, provider *emailProvider) error {
	key := "welcome:" + uuid.NewString()
	for n := 1; n <= 3; n++ {
		line, err := attempt(ctx, conn, sender, provider, key, "ada@example.com")
		if err != nil {
			return err
		}
		fmt.Printf("  attempt %d: %s\n", n, line)
	}
	return nil
}
