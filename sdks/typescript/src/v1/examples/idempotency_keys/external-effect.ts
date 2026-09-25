import { randomUUID } from 'crypto';
import { claim } from './claim';
import { Queryable } from './db';

export type SendResult = { messageId: string };

export type EmailSender = {
  send(to: string, idempotencyKey?: string): SendResult;
};

// > stub provider
export class EmailProvider implements EmailSender {
  deliveries: string[] = [];

  private resultsByKey = new Map<string, SendResult>();

  send(to: string, idempotencyKey?: string): SendResult {
    if (idempotencyKey !== undefined && this.resultsByKey.has(idempotencyKey)) {
      return this.resultsByKey.get(idempotencyKey)!;
    }

    this.deliveries.push(to);
    const result = { messageId: randomUUID() };

    if (idempotencyKey !== undefined) {
      this.resultsByKey.set(idempotencyKey, result);
    }

    return result;
  }
}
// !!

// > external effect
export async function sendWelcomeEmail(
  db: Queryable,
  provider: EmailSender,
  key: string,
  to: string
) {
  const recorded = await db.query('SELECT 1 FROM processed_job WHERE idempotency_key = $1', [key]);

  if (recorded.rows.length > 0) {
    return false;
  }

  provider.send(to, key);

  await claim(db, key);

  return true;
}
// !!
