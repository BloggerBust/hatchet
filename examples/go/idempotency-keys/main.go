package main

import (
	"context"
	"fmt"
	"log"
)

func main() {
	ctx := context.Background()
	conn, err := connect(ctx)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, schema); err != nil {
		log.Fatalf("failed to create tables: %v", err)
	}

	fmt.Println("provider honors the idempotency key")
	provider := newEmailProvider()
	if err := lostRunner(ctx, conn, &losesFirstResponse{inner: provider}, provider); err != nil {
		log.Fatal(err)
	}

	fmt.Println("provider ignores the idempotency key")
	provider = newEmailProvider()
	if err := lostRunner(ctx, conn, &losesFirstResponse{inner: &ignoresIdempotencyKey{inner: provider}}, provider); err != nil {
		log.Fatal(err)
	}
}
