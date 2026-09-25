from typing import Any

import psycopg

from examples.idempotency_keys.local_effect import SIGNUP_CREDIT


# > check then insert
def grant_signup_credit_naive(
    conn: psycopg.Connection[Any], key: str, user_id: str
) -> bool:
    already_processed = conn.execute(
        "SELECT 1 FROM processed_job WHERE idempotency_key = %s", (key,)
    ).fetchone()

    if already_processed:
        return False

    conn.execute(
        "INSERT INTO account_credit (user_id, amount) VALUES (%s, %s)",
        (user_id, SIGNUP_CREDIT),
    )

    conn.execute("INSERT INTO processed_job (idempotency_key) VALUES (%s)", (key,))

    return True


