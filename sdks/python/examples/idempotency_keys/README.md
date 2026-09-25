# Idempotency keys for background jobs

Example code and tests for the blog post "Idempotency keys for background jobs". The same examples exist in three languages:

- Python: `sdks/python/examples/idempotency_keys` (this directory)
- TypeScript: `sdks/typescript/src/v1/examples/idempotency_keys`
- Go: `sdks/go/examples/idempotency-keys`

## Start a Postgres

```sh
docker run --rm -d --name idempotency-keys-pg \
  -e POSTGRES_USER=example -e POSTGRES_PASSWORD=example -e POSTGRES_DB=idempotency_keys \
  -p 5499:5432 postgres:15
export DATABASE_URL=postgresql://example:example@localhost:5499/idempotency_keys
```

## Run the tests

Each language has one test file covering the examples in the article, including a simulation of Gray's lost runner with and without provider-side idempotency.

### Python

```sh
cd sdks/python
python3 -m venv .venv
.venv/bin/pip install "psycopg[binary]" pytest hatchet-sdk
.venv/bin/python -m pytest examples/idempotency_keys --noconftest
```

### TypeScript

```sh
cd sdks/typescript
pnpm install
pnpm test:unit --testPathPattern=idempotency_keys
```

### Go

```sh
cd sdks/go/examples/idempotency-keys
go test -v .
```

## Simulate the lost runner

Each language has a runnable demonstration of Gray's lost runner. It attempts the welcome-email operation three times with a provider that supports idempotency, then repeats the experiment with one that does not.

Python, from `sdks/python` with the virtualenv above:

```sh
.venv/bin/python -m examples.idempotency_keys.lost_runner
```

TypeScript, from `sdks/typescript` after `pnpm install`:

```sh
pnpm exec tsx src/v1/examples/idempotency_keys/lost-runner.ts
```

Go:

```sh
cd sdks/go/examples/idempotency-keys
go run .
```

Every language prints the same sequence:

```
provider honors the idempotency key
  attempt 1: response lost (record: absent, deliveries: 1)
  attempt 2: called the provider (record: present, deliveries: 1)
  attempt 3: already recorded (record: present, deliveries: 1)
provider ignores the idempotency key
  attempt 1: response lost (record: absent, deliveries: 1)
  attempt 2: called the provider (record: present, deliveries: 2)
  attempt 3: already recorded (record: present, deliveries: 2)
```

## Run the trigger-level example

Install the Hatchet CLI and start a local instance with authentication disabled:

```sh
curl -fsSL https://install.hatchet.run/install.sh | bash
hatchet server start --disable-auth
```

Load the Hatchet profile in each shell:

```sh
eval "$(hatchet profile env)"
```

Each example uses the same idempotency key with a one-minute TTL, so wait a minute before switching languages.

For Python and TypeScript, start the worker in one terminal and the trigger in another. The trigger prints the collision message for its second trigger.

Python, from `sdks/python` with the virtualenv above:

```sh
.venv/bin/python -m examples.idempotency.worker
.venv/bin/python -m examples.idempotency.trigger
```

TypeScript, from `sdks/typescript` after `pnpm install`:

```sh
pnpm exec tsx src/v1/examples/idempotency/worker.ts
pnpm exec tsx src/v1/examples/idempotency/run.ts
```

The TypeScript trigger keeps its client connection open after it prints the result, so stop it with Ctrl-C.

Go, where the worker and the triggers run in the same process:

```sh
cd sdks/go/examples/idempotency
go run .
```

## Clean up

```sh
hatchet server stop
docker stop idempotency-keys-pg
```
