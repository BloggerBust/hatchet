import { Client } from 'pg';

export type Queryable = {
  query(sql: string, params?: unknown[]): Promise<{ rows: unknown[] }>;
};

export const SCHEMA = `
CREATE TABLE IF NOT EXISTS processed_job (idempotency_key text PRIMARY KEY);
CREATE TABLE IF NOT EXISTS account_credit (user_id text NOT NULL, amount integer NOT NULL);
`;

export async function connect() {
  const client = new Client({ connectionString: process.env.DATABASE_URL });
  await client.connect();
  return client;
}
