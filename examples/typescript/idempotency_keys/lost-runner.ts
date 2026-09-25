import { randomUUID } from 'crypto';
import { connect, Queryable, SCHEMA } from './db';
import { EmailProvider, EmailSender, SendResult, sendWelcomeEmail } from './external-effect';

export class LostResponse extends Error {}

export class LosesFirstResponse implements EmailSender {
  private lost = false;

  constructor(private readonly inner: EmailSender) {}

  send(to: string, idempotencyKey?: string): SendResult {
    const result = this.inner.send(to, idempotencyKey);
    if (!this.lost) {
      this.lost = true;
      throw new LostResponse();
    }
    return result;
  }
}

export class IgnoresIdempotencyKey implements EmailSender {
  constructor(private readonly inner: EmailSender) {}

  send(to: string): SendResult {
    return this.inner.send(to);
  }
}

async function attempt(
  db: Queryable,
  sender: EmailSender,
  provider: EmailProvider,
  key: string,
  to: string
) {
  let outcome: string;
  try {
    outcome = (await sendWelcomeEmail(db, sender, key, to))
      ? 'called the provider'
      : 'already recorded';
  } catch (e) {
    if (!(e instanceof LostResponse)) {
      throw e;
    }
    outcome = 'response lost';
  }

  const recorded = await db.query('SELECT 1 FROM processed_job WHERE idempotency_key = $1', [key]);
  const record = recorded.rows.length > 0 ? 'present' : 'absent';

  return `${outcome} (record: ${record}, deliveries: ${provider.deliveries.length})`;
}

export async function lostRunner(db: Queryable, sender: EmailSender, provider: EmailProvider) {
  const key = `welcome:${randomUUID()}`;
  for (let n = 1; n <= 3; n += 1) {
    const line = await attempt(db, sender, provider, key, 'ada@example.com');
    console.log(`  attempt ${n}: ${line}`);
  }
}

async function main() {
  const db = await connect();
  try {
    await db.query(SCHEMA);

    console.log('provider honors the idempotency key');
    let provider = new EmailProvider();
    await lostRunner(db, new LosesFirstResponse(provider), provider);

    console.log('provider ignores the idempotency key');
    provider = new EmailProvider();
    await lostRunner(db, new LosesFirstResponse(new IgnoresIdempotencyKey(provider)), provider);
  } finally {
    await db.end();
  }
}

if (require.main === module) {
  main().catch((e) => {
    console.error(e);
    process.exit(1);
  });
}
