import { claim } from './claim';
import { Queryable } from './db';

// > local effect
export const SIGNUP_CREDIT = 10;

export async function grantSignupCredit(tx: Queryable, key: string, userId: string) {
  if (!(await claim(tx, key))) {
    return false;
  }

  await tx.query('INSERT INTO account_credit (user_id, amount) VALUES ($1, $2)', [
    userId,
    SIGNUP_CREDIT,
  ]);

  return true;
}
// !!
