from dataclasses import dataclass, field
from typing import Any, Protocol
from uuid import uuid4

import psycopg

from examples.idempotency_keys.claim import claim


@dataclass
class SendResult:
    message_id: str


class EmailSender(Protocol):
    def send(self, to: str, idempotency_key: str | None = None) -> SendResult: ...


# > stub provider
@dataclass
class EmailProvider:
    deliveries: list[str] = field(default_factory=list)
    results_by_key: dict[str, SendResult] = field(default_factory=dict)

    def send(self, to: str, idempotency_key: str | None = None) -> SendResult:
        if idempotency_key is not None and idempotency_key in self.results_by_key:
            return self.results_by_key[idempotency_key]

        self.deliveries.append(to)
        result = SendResult(message_id=str(uuid4()))

        if idempotency_key is not None:
            self.results_by_key[idempotency_key] = result

        return result


# !!


# > external effect
def send_welcome_email(
    conn: psycopg.Connection[Any], provider: EmailSender, key: str, to: str
) -> bool:
    already_recorded = conn.execute(
        "SELECT 1 FROM processed_job WHERE idempotency_key = %s", (key,)
    ).fetchone()

    if already_recorded:
        return False

    provider.send(to, idempotency_key=key)

    claim(conn, key)

    return True


# !!
