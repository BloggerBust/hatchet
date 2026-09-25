import threading
from collections.abc import Callable, Generator
from typing import Any
from uuid import uuid4

import psycopg
import pytest

from examples.idempotency_keys.claim import claim
from examples.idempotency_keys.db import DATABASE_URL, SCHEMA, connect
from examples.idempotency_keys.external_effect import (
    EmailProvider,
    EmailSender,
    send_welcome_email,
)
from examples.idempotency_keys.local_effect import SIGNUP_CREDIT, grant_signup_credit
from examples.idempotency_keys.lost_runner import (
    IgnoresIdempotencyKey,
    LosesFirstResponse,
    LostResponseError,
)
from examples.idempotency_keys.naive import grant_signup_credit_naive

LOCK_WAIT_POLLS = 200
LOCK_WAIT_POLL_SECONDS = 0.05


class WorkerDiedError(Exception):
    pass


@pytest.fixture(scope="module")
def schema() -> None:
    if DATABASE_URL is None:
        pytest.skip("DATABASE_URL is not set")
    with connect() as conn:
        conn.execute(SCHEMA)


@pytest.fixture
def conn(schema: None) -> Generator[psycopg.Connection[Any], None, None]:
    with connect() as c:
        yield c


@pytest.fixture
def other_conn(schema: None) -> Generator[psycopg.Connection[Any], None, None]:
    with connect() as c:
        yield c


def _run_two_workers(
    worker: Callable[[str], None],
) -> dict[str, BaseException | None]:
    errors: dict[str, BaseException | None] = {"A": None, "B": None}

    def run(name: str) -> None:
        try:
            worker(name)
        except BaseException as e:
            errors[name] = e

    threads = [threading.Thread(target=run, args=(name,)) for name in ("A", "B")]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    return errors


def _wait_until_blocked_on_lock(conn: psycopg.Connection[Any], pid: int) -> None:
    for _ in range(LOCK_WAIT_POLLS):
        row = conn.execute(
            "SELECT wait_event_type FROM pg_stat_activity WHERE pid = %s", (pid,)
        ).fetchone()
        if row and row[0] == "Lock":
            return
        threading.Event().wait(LOCK_WAIT_POLL_SECONDS)
    raise AssertionError(f"backend {pid} never blocked on a lock")


def _row_count(conn: psycopg.Connection[Any], key: str) -> int:
    row = conn.execute(
        "SELECT count(*) FROM processed_job WHERE idempotency_key = %s", (key,)
    ).fetchone()
    assert row is not None
    return int(row[0])


def _credits(conn: psycopg.Connection[Any], user_id: str) -> int:
    row = conn.execute(
        "SELECT coalesce(sum(amount), 0) FROM account_credit WHERE user_id = %s",
        (user_id,),
    ).fetchone()
    assert row is not None
    return int(row[0])


def test_check_then_insert_grants_the_credit_twice_under_concurrency(
    conn: psycopg.Connection[Any], other_conn: psycopg.Connection[Any]
) -> None:
    key = f"signup-credit:{uuid4()}"
    user_id = str(uuid4())
    conns = {"A": conn, "B": other_conn}
    errors: dict[str, BaseException | None] = {"A": None, "B": None}

    def run(name: str) -> None:
        try:
            grant_signup_credit_naive(conns[name], key, user_id)
        except BaseException as e:
            errors[name] = e

    threads = [threading.Thread(target=run, args=(name,)) for name in ("A", "B")]

    with connect() as blocker, blocker.transaction():
        blocker.execute("LOCK TABLE account_credit IN ACCESS EXCLUSIVE MODE")
        for t in threads:
            t.start()
        for c in conns.values():
            _wait_until_blocked_on_lock(blocker, c.info.backend_pid)

    for t in threads:
        t.join()

    assert (
        sum(isinstance(e, psycopg.errors.UniqueViolation) for e in errors.values()) == 1
    )
    assert _row_count(conn, key) == 1
    assert _credits(conn, user_id) == 2 * SIGNUP_CREDIT


def test_claim_is_won_by_exactly_one_of_two_concurrent_workers(
    conn: psycopg.Connection[Any], other_conn: psycopg.Connection[Any]
) -> None:
    key = f"welcome:{uuid4()}"
    both_ready = threading.Barrier(2)
    won: dict[str, bool] = {}
    conns = {"A": conn, "B": other_conn}

    def worker(name: str) -> None:
        both_ready.wait()
        won[name] = claim(conns[name], key)

    errors = _run_two_workers(worker)

    assert errors == {"A": None, "B": None}
    assert sorted(won.values()) == [False, True]


def _second_claim_after_first(
    conn: psycopg.Connection[Any],
    other_conn: psycopg.Connection[Any],
    first_outcome: str,
) -> dict[str, bool]:
    key = f"welcome:{uuid4()}"
    first_claimed = threading.Event()
    release_first = threading.Event()
    outcomes: dict[str, bool] = {}

    def first() -> None:
        try:
            with conn.transaction():
                outcomes["first"] = claim(conn, key)
                first_claimed.set()
                release_first.wait()
                if first_outcome == "rollback":
                    raise WorkerDiedError
        except WorkerDiedError:
            pass

    def second() -> None:
        outcomes["second"] = claim(other_conn, key)

    first_thread = threading.Thread(target=first)
    second_thread = threading.Thread(target=second)
    first_thread.start()
    first_claimed.wait()
    second_thread.start()

    with connect() as observer:
        _wait_until_blocked_on_lock(observer, other_conn.info.backend_pid)

    assert "second" not in outcomes
    release_first.set()
    first_thread.join()
    second_thread.join()

    return outcomes


def test_second_claim_waits_then_loses_if_first_commits(
    conn: psycopg.Connection[Any], other_conn: psycopg.Connection[Any]
) -> None:
    assert _second_claim_after_first(conn, other_conn, "commit") == {
        "first": True,
        "second": False,
    }


def test_second_claim_waits_then_wins_if_first_rolls_back(
    conn: psycopg.Connection[Any], other_conn: psycopg.Connection[Any]
) -> None:
    assert _second_claim_after_first(conn, other_conn, "rollback") == {
        "first": True,
        "second": True,
    }


def test_local_effect_rolls_back_with_claim_and_commits_once(
    conn: psycopg.Connection[Any],
) -> None:
    key = f"signup-credit:{uuid4()}"
    user_id = str(uuid4())

    with pytest.raises(WorkerDiedError), conn.transaction():
        assert grant_signup_credit(conn, key, user_id) is True
        raise WorkerDiedError

    assert _row_count(conn, key) == 0
    assert _credits(conn, user_id) == 0

    assert grant_signup_credit(conn, key, user_id) is True
    assert grant_signup_credit(conn, key, user_id) is False
    assert _credits(conn, user_id) == SIGNUP_CREDIT


def _deliveries_after_lost_response_then_retry(
    conn: psycopg.Connection[Any], sender: EmailSender, provider: EmailProvider
) -> list[str]:
    key = f"welcome:{uuid4()}"

    with pytest.raises(LostResponseError):
        send_welcome_email(conn, sender, key, "ada@example.com")

    assert provider.deliveries == ["ada@example.com"]
    assert _row_count(conn, key) == 0

    assert send_welcome_email(conn, sender, key, "ada@example.com") is True
    assert _row_count(conn, key) == 1

    assert send_welcome_email(conn, sender, key, "ada@example.com") is False

    return provider.deliveries


def test_external_effect_with_key_is_delivered_once_after_lost_response(
    conn: psycopg.Connection[Any],
) -> None:
    provider = EmailProvider()
    sender = LosesFirstResponse(inner=provider)

    assert _deliveries_after_lost_response_then_retry(conn, sender, provider) == [
        "ada@example.com"
    ]


def test_external_effect_without_key_is_delivered_twice_after_lost_response(
    conn: psycopg.Connection[Any],
) -> None:
    provider = EmailProvider()
    sender = LosesFirstResponse(inner=IgnoresIdempotencyKey(inner=provider))

    assert _deliveries_after_lost_response_then_retry(conn, sender, provider) == [
        "ada@example.com",
        "ada@example.com",
    ]
