import { hatchet } from '../hatchet-client';
import { idempotentTask } from './workflow';

async function main() {
  const worker = await hatchet.worker('idempotency-worker', {
    workflows: [idempotentTask],
  });

  await worker.start();
}

if (require.main === module) {
  main();
}
