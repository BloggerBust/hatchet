import { Queryable } from './db';
import { SIGNUP_CREDIT } from './local-effect';

// > check then insert
export async function grantSignupCreditNaive(db: Queryable, key: string, userId: string) {
  const existing = await db.query('SELECT 1 FROM processed_job WHERE idempotency_key = $1', [key]);

  if (existing.rows.length > 0) {
    return false;
  }

  await db.query('INSERT INTO account_credit (user_id, amount) VALUES ($1, $2)', [
    userId,
    SIGNUP_CREDIT,
  ]);

  await db.query('INSERT INTO processed_job (idempotency_key) VALUES ($1)', [key]);

  return true;
}
