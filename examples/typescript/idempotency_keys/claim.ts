import { Queryable } from './db';

// > atomic claim
export async function claim(db: Queryable, key: string): Promise<boolean> {
  const result = await db.query(
    `INSERT INTO processed_job (idempotency_key)
     VALUES ($1)
     ON CONFLICT (idempotency_key) DO NOTHING
     RETURNING idempotency_key`,
    [key]
  );

  return result.rows.length === 1;
}
