from typing import Any

import psycopg

from examples.idempotency_keys.claim import claim

# > local effect
SIGNUP_CREDIT = 10


def grant_signup_credit(conn: psycopg.Connection[Any], key: str, user_id: str) -> bool:
    with conn.transaction():
        if not claim(conn, key):
            return False

        conn.execute(
            "INSERT INTO account_credit (user_id, amount) VALUES (%s, %s)",
            (user_id, SIGNUP_CREDIT),
        )

    return True


# !!
