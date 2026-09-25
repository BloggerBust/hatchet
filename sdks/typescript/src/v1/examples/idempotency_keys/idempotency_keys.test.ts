import { randomUUID } from 'crypto';
import { Client } from 'pg';
import { claim } from './claim';
import { connect, SCHEMA } from './db';
import { EmailProvider, EmailSender, sendWelcomeEmail } from './external-effect';
import { grantSignupCredit, SIGNUP_CREDIT } from './local-effect';
import { IgnoresIdempotencyKey, LosesFirstResponse, LostResponse } from './lost-runner';
import { grantSignupCreditNaive } from './naive';

const UNIQUE_VIOLATION = '23505';
const LOCK_WAIT_POLLS = 200;
const LOCK_WAIT_POLL_MS = 50;

const describeWithDatabase = process.env.DATABASE_URL ? describe : describe.skip;

function latch(count: number) {
  let remaining = count;
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  return async () => {
    remaining -= 1;
    if (remaining === 0) release();
    await released;
  };
}

async function inTransaction<T>(
  client: Client,
  body: () => Promise<T>,
  outcome: 'COMMIT' | 'ROLLBACK' = 'COMMIT'
) {
  await client.query('BEGIN');
  try {
    const result = await body();
    await client.query(outcome);
    return result;
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  }
}

async function rowCount(client: Client, key: string) {
  const result = await client.query(
    'SELECT count(*)::int AS n FROM processed_job WHERE idempotency_key = $1',
    [key]
  );
  return result.rows[0].n as number;
}

async function backendPid(client: Client) {
  return (await client.query('SELECT pg_backend_pid() AS pid')).rows[0].pid as number;
}

async function creditsFor(client: Client, userId: string) {
  const result = await client.query(
    'SELECT coalesce(sum(amount), 0)::int AS total FROM account_credit WHERE user_id = $1',
    [userId]
  );
  return result.rows[0].total as number;
}

async function waitUntilBlockedOnLock(observer: Client, pid: number) {
  for (let i = 0; i < LOCK_WAIT_POLLS; i += 1) {
    const result = await observer.query(
      'SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1',
      [pid]
    );
    if (result.rows[0]?.wait_event_type === 'Lock') return;
    await new Promise((resolve) => {
      setTimeout(resolve, LOCK_WAIT_POLL_MS);
    });
  }
  throw new Error(`backend ${pid} never blocked on a lock`);
}

describeWithDatabase('idempotency keys', () => {
  let a: Client;
  let b: Client;
  let observer: Client;

  beforeAll(async () => {
    a = await connect();
    b = await connect();
    observer = await connect();
    await a.query(SCHEMA);
  });

  afterAll(async () => {
    await Promise.all([a.end(), b.end(), observer.end()]);
  });

  it('check-then-insert grants the credit twice under concurrency', async () => {
    const key = `signup-credit:${randomUUID()}`;
    const userId = randomUUID();
    const pids = await Promise.all([a, b].map(async (client) => await backendPid(client)));

    await observer.query('BEGIN');
    await observer.query('LOCK TABLE account_credit IN ACCESS EXCLUSIVE MODE');
    const results = Promise.all(
      [a, b].map((client) =>
        grantSignupCreditNaive(client, key, userId).then(
          () => null,
          (e: { code?: string }) => e
        )
      )
    );
    for (const pid of pids) {
      await waitUntilBlockedOnLock(observer, pid);
    }
    await observer.query('ROLLBACK');
    const settled = await results;

    expect(settled.filter((e) => e?.code === UNIQUE_VIOLATION)).toHaveLength(1);
    expect(await rowCount(a, key)).toBe(1);
    expect(await creditsFor(a, userId)).toBe(2 * SIGNUP_CREDIT);
  });

  it('claim is won by exactly one of two concurrent workers', async () => {
    const key = `welcome:${randomUUID()}`;
    const bothReady = latch(2);

    const won = await Promise.all(
      [a, b].map(async (client) => {
        await bothReady();
        return claim(client, key);
      })
    );

    expect(won.sort()).toEqual([false, true]);
  });

  it.each([
    ['COMMIT', false],
    ['ROLLBACK', true],
  ] as const)(
    'second claim waits, then after %s returns %s',
    async (firstOutcome, expectedSecond) => {
      const key = `welcome:${randomUUID()}`;
      await a.query('BEGIN');
      expect(await claim(a, key)).toBe(true);

      const pid = await backendPid(b);

      let secondDone = false;
      const second = claim(b, key).then((claimed) => {
        secondDone = true;
        return claimed;
      });

      await waitUntilBlockedOnLock(observer, pid);
      expect(secondDone).toBe(false);

      await a.query(firstOutcome);
      expect(await second).toBe(expectedSecond);
    }
  );

  it('local effect rolls back with the claim and commits once', async () => {
    const key = `signup-credit:${randomUUID()}`;
    const userId = randomUUID();

    expect(await inTransaction(a, () => grantSignupCredit(a, key, userId), 'ROLLBACK')).toBe(true);
    expect(await rowCount(a, key)).toBe(0);
    expect(await creditsFor(a, userId)).toBe(0);

    expect(await inTransaction(a, () => grantSignupCredit(a, key, userId))).toBe(true);
    expect(await inTransaction(a, () => grantSignupCredit(a, key, userId))).toBe(false);
    expect(await creditsFor(a, userId)).toBe(SIGNUP_CREDIT);
  });

  async function deliveriesAfterLostResponseThenRetry(
    sender: EmailSender,
    provider: EmailProvider
  ) {
    const key = `welcome:${randomUUID()}`;
    const to = 'ada@example.com';

    await expect(sendWelcomeEmail(a, sender, key, to)).rejects.toBeInstanceOf(LostResponse);
    expect(provider.deliveries).toEqual([to]);
    expect(await rowCount(a, key)).toBe(0);

    expect(await sendWelcomeEmail(a, sender, key, to)).toBe(true);
    expect(await rowCount(a, key)).toBe(1);
    expect(await sendWelcomeEmail(a, sender, key, to)).toBe(false);

    return provider.deliveries;
  }

  it('external effect with key is delivered once after a lost response', async () => {
    const provider = new EmailProvider();
    const sender = new LosesFirstResponse(provider);

    expect(await deliveriesAfterLostResponseThenRetry(sender, provider)).toEqual([
      'ada@example.com',
    ]);
  });

  it('external effect without key is delivered twice after a lost response', async () => {
    const provider = new EmailProvider();
    const sender = new LosesFirstResponse(new IgnoresIdempotencyKey(provider));

    expect(await deliveriesAfterLostResponseThenRetry(sender, provider)).toEqual([
      'ada@example.com',
      'ada@example.com',
    ]);
  });
});
