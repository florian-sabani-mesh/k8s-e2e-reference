import { beforeEach, afterEach } from 'vitest';
import { mkdir, writeFile } from 'node:fs/promises';
import { wiremock } from './wiremock.js';
beforeEach(async () => { await wiremock.reset(); });
afterEach(async (context) => {
 if (context.task.result?.state === 'fail') {
  const path = new URL('../../artifacts/', import.meta.url); await mkdir(path, { recursive: true });
  const name = context.task.name.replace(/[^a-z0-9]+/gi, '-');
  await writeFile(new URL(`wiremock-${name}.json`, path), JSON.stringify(await wiremock.snapshot(), null, 2));
 }
});
