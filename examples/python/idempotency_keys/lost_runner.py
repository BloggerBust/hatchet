from dataclasses import dataclass
from typing import Any
from uuid import uuid4

import psycopg

from examples.idempotency_keys.db import SCHEMA, connect
from examples.idempotency_keys.external_effect import (
    EmailProvider,
    EmailSender,
    SendResult,
    send_welcome_email,
)


class LostResponseError(Exception):
    pass


@dataclass
class LosesFirstResponse:
    inner: EmailSender
    lost: bool = False

    def send(self, to: str, idempotency_key: str | None = None) -> SendResult:
        result = self.inner.send(to, idempotency_key)
        if not self.lost:
            self.lost = True
            raise LostResponseError
        return result


@dataclass
class IgnoresIdempotencyKey:
    inner: EmailSender

    def send(self, to: str, idempotency_key: str | None = None) -> SendResult:
        return self.inner.send(to, None)


def attempt(
    conn: psycopg.Connection[Any],
    sender: EmailSender,
    provider: EmailProvider,
    key: str,
    to: str,
) -> str:
    try:
        called = send_welcome_email(conn, sender, key, to)
    except LostResponseError:
        outcome = "response lost"
    else:
        outcome = "called the provider" if called else "already recorded"

    recorded = conn.execute(
        "SELECT 1 FROM processed_job WHERE idempotency_key = %s", (key,)
    ).fetchone()
    record = "present" if recorded else "absent"

    return f"{outcome} (record: {record}, deliveries: {len(provider.deliveries)})"


def lost_runner(
    conn: psycopg.Connection[Any], sender: EmailSender, provider: EmailProvider
) -> None:
    key = f"welcome:{uuid4()}"
    for n in range(1, 4):
        print(
            f"  attempt {n}: {attempt(conn, sender, provider, key, 'ada@example.com')}"
        )


def main() -> None:
    with connect() as conn:
        conn.execute(SCHEMA)

        print("provider honors the idempotency key")
        provider = EmailProvider()
        lost_runner(conn, LosesFirstResponse(inner=provider), provider)

        print("provider ignores the idempotency key")
        provider = EmailProvider()
        lost_runner(
            conn,
            LosesFirstResponse(inner=IgnoresIdempotencyKey(inner=provider)),
            provider,
        )


if __name__ == "__main__":
    main()
