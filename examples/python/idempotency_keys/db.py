import os
from typing import Any

import psycopg

DATABASE_URL = os.environ.get("DATABASE_URL")

SCHEMA = """
CREATE TABLE IF NOT EXISTS processed_job (
    idempotency_key text PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS account_credit (
    user_id text NOT NULL,
    amount integer NOT NULL
);
"""


def connect() -> psycopg.Connection[Any]:
    if DATABASE_URL is None:
        raise RuntimeError("DATABASE_URL is not set")
    return psycopg.connect(DATABASE_URL, autocommit=True)
