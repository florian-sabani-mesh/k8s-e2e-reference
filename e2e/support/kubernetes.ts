import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
const run = promisify(execFile);
const kubeconfig = process.env.KUBECONFIG ?? fileURLToPath(new URL('../../.run/kubeconfig', import.meta.url));
export async function kubectl(...args: string[]) {
 const { stdout } = await run('kubectl', ['--kubeconfig', kubeconfig, '--context', 'kind-terraform-k8s-e2e-reference', '-n', 'e2e', ...args], { timeout: 20_000 });
 return stdout;
}
export type Pod = { metadata: { name: string; uid: string; deletionTimestamp?: string }; status: { conditions?: Array<{ type: string; status: string }> } };
export async function pods(app: string): Promise<Pod[]> {
 const result = JSON.parse(await kubectl('get', 'pods', '-l', `app=${app}`, '-o', 'json')) as { items: Pod[] };
 return result.items.filter(pod => !pod.metadata.deletionTimestamp);
}
export function isReady(pod: Pod) { return pod.status.conditions?.some(condition => condition.type === 'Ready' && condition.status === 'True') ?? false; }
