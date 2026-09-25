from typing import Any

import psycopg


# > atomic claim
def claim(conn: psycopg.Connection[Any], key: str) -> bool:
    row = conn.execute(
        """
        INSERT INTO processed_job (idempotency_key)
        VALUES (%s)
        ON CONFLICT (idempotency_key) DO NOTHING
        RETURNING idempotency_key
        """,
        (key,),
    ).fetchone()

    return row is not None


