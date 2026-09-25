package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	uniqueViolation     = "23505"
	lockWaitPolls       = 200
	lockWaitPollSeconds = 50 * time.Millisecond
)

func testConn(t *testing.T) *pgx.Conn {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	conn, err := connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(context.Background(), schema); err != nil {
		t.Fatal(err)
	}
	return conn
}

func rowCount(t *testing.T, conn *pgx.Conn, key string) int {
	t.Helper()
	var n int
	err := conn.QueryRow(context.Background(), "SELECT count(*) FROM processed_job WHERE idempotency_key = $1", key).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func barrier(n int) func() {
	var wg sync.WaitGroup
	wg.Add(n)
	return func() {
		wg.Done()
		wg.Wait()
	}
}

func waitUntilBlockedOnLock(t *testing.T, observer *pgx.Conn, pid uint32) {
	t.Helper()
	for range lockWaitPolls {
		var waitEventType *string
		err := observer.QueryRow(context.Background(), "SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1", pid).Scan(&waitEventType)
		if err == nil && waitEventType != nil && *waitEventType == "Lock" {
			return
		}
		time.Sleep(lockWaitPollSeconds)
	}
	t.Fatalf("backend %d never blocked on a lock", pid)
}

func credits(t *testing.T, conn *pgx.Conn, userID string) int {
	t.Helper()
	var total int
	err := conn.QueryRow(context.Background(), "SELECT coalesce(sum(amount), 0) FROM account_credit WHERE user_id = $1", userID).Scan(&total)
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func TestCheckThenInsertGrantsTheCreditTwiceUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	conns := map[string]*pgx.Conn{"A": testConn(t), "B": testConn(t)}
	blocker, observer := testConn(t), testConn(t)
	key := "signup-credit:" + uuid.NewString()
	userID := uuid.NewString()

	lockTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockTx.Exec(ctx, "LOCK TABLE account_credit IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	errs := map[string]error{}
	var wg sync.WaitGroup

	for name, conn := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := grantSignupCreditNaive(ctx, conn, key, userID)
			mu.Lock()
			errs[name] = err
			mu.Unlock()
		}()
	}
	for _, conn := range conns {
		waitUntilBlockedOnLock(t, observer, conn.PgConn().PID())
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	violations := 0
	for _, err := range errs {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			violations++
		}
	}
	if violations != 1 {
		t.Fatalf("expected one unique violation, got %d (%v)", violations, errs)
	}
	if n := rowCount(t, conns["A"], key); n != 1 {
		t.Fatalf("expected one row, got %d", n)
	}
	if c := credits(t, conns["A"], userID); c != 2*signupCredit {
		t.Fatalf("expected the credit to be granted twice (%d), got %d", 2*signupCredit, c)
	}
}

func TestClaimIsWonByExactlyOneOfTwoConcurrentWorkers(t *testing.T) {
	ctx := context.Background()
	conns := []*pgx.Conn{testConn(t), testConn(t)}
	key := "welcome:" + uuid.NewString()
	bothReady := barrier(2)

	won := make([]bool, 2)
	var wg sync.WaitGroup
	for i, conn := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bothReady()
			claimed, err := claim(ctx, conn, key)
			if err != nil {
				t.Error(err)
			}
			won[i] = claimed
		}()
	}
	wg.Wait()

	if won[0] == won[1] {
		t.Fatalf("expected exactly one winner, got %v", won)
	}
}

func secondClaimAfterFirst(t *testing.T, firstCommits bool) (first, second bool) {
	t.Helper()
	ctx := context.Background()
	firstConn, secondConn, observer := testConn(t), testConn(t), testConn(t)
	key := "welcome:" + uuid.NewString()

	tx, err := firstConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err = claim(ctx, tx, key)
	if err != nil {
		t.Fatal(err)
	}

	secondDone := make(chan bool, 1)
	go func() {
		claimed, err := claim(ctx, secondConn, key)
		if err != nil {
			t.Error(err)
		}
		secondDone <- claimed
	}()

	waitUntilBlockedOnLock(t, observer, secondConn.PgConn().PID())
	select {
	case <-secondDone:
		t.Fatal("second claim returned while the first transaction was still open")
	default:
	}

	if firstCommits {
		err = tx.Commit(ctx)
	} else {
		err = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	return first, <-secondDone
}

func TestSecondClaimWaitsThenLosesIfFirstCommits(t *testing.T) {
	first, second := secondClaimAfterFirst(t, true)
	if !first || second {
		t.Fatalf("expected first=true second=false, got first=%v second=%v", first, second)
	}
}

func TestSecondClaimWaitsThenWinsIfFirstRollsBack(t *testing.T) {
	first, second := secondClaimAfterFirst(t, false)
	if !first || !second {
		t.Fatalf("expected first=true second=true, got first=%v second=%v", first, second)
	}
}

func TestLocalEffectRollsBackWithClaimAndCommitsOnce(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t)
	key := "signup-credit:" + uuid.NewString()
	userID := uuid.NewString()

	grant := func(finish func(pgx.Tx) error) bool {
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		granted, err := grantSignupCredit(ctx, tx, key, userID)
		if err != nil {
			t.Fatal(err)
		}
		if err := finish(tx); err != nil {
			t.Fatal(err)
		}
		return granted
	}
	rollback := func(tx pgx.Tx) error { return tx.Rollback(ctx) }
	commit := func(tx pgx.Tx) error { return tx.Commit(ctx) }

	if !grant(rollback) {
		t.Fatal("expected the first attempt to claim the key")
	}
	if n, c := rowCount(t, conn, key), credits(t, conn, userID); n != 0 || c != 0 {
		t.Fatalf("expected rollback to remove claim and credit, got rows=%d credits=%d", n, c)
	}

	if !grant(commit) {
		t.Fatal("expected the retry to claim the key")
	}
	if grant(commit) {
		t.Fatal("expected the duplicate to find the key claimed")
	}
	if c := credits(t, conn, userID); c != signupCredit {
		t.Fatalf("expected credit %d, got %d", signupCredit, c)
	}
}

func deliveriesAfterLostResponseThenRetry(t *testing.T, sender emailSender, provider *emailProvider) []string {
	t.Helper()
	ctx := context.Background()
	conn := testConn(t)
	key := "welcome:" + uuid.NewString()
	to := "ada@example.com"

	if _, err := sendWelcomeEmail(ctx, conn, sender, key, to); !errors.Is(err, errLostResponse) {
		t.Fatalf("expected the lost response, got %v", err)
	}
	if len(provider.deliveries) != 1 || rowCount(t, conn, key) != 0 {
		t.Fatalf("expected one delivery and no record after the lost response")
	}

	retried, err := sendWelcomeEmail(ctx, conn, sender, key, to)
	if err != nil || !retried {
		t.Fatalf("expected the retry to call the provider, got %v %v", retried, err)
	}
	if rowCount(t, conn, key) != 1 {
		t.Fatal("expected the retry to record the key")
	}

	third, err := sendWelcomeEmail(ctx, conn, sender, key, to)
	if err != nil || third {
		t.Fatalf("expected the third attempt to stop at the local record, got %v %v", third, err)
	}
	return provider.deliveries
}

func TestExternalEffectWithKeyIsDeliveredOnceAfterLostResponse(t *testing.T) {
	provider := newEmailProvider()
	sender := &losesFirstResponse{inner: provider}

	if got := deliveriesAfterLostResponseThenRetry(t, sender, provider); len(got) != 1 {
		t.Fatalf("expected one delivery, got %v", got)
	}
}

func TestExternalEffectWithoutKeyIsDeliveredTwiceAfterLostResponse(t *testing.T) {
	provider := newEmailProvider()
	sender := &losesFirstResponse{inner: &ignoresIdempotencyKey{inner: provider}}

	if got := deliveriesAfterLostResponseThenRetry(t, sender, provider); len(got) != 2 {
		t.Fatalf("expected two deliveries, got %v", got)
	}
}
