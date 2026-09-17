import { randomUUID } from 'node:crypto';
export type Matcher = string | { equalTo?: string; matches?: string; contains?: string; absent?: boolean };
export type BodyMatcher = { equalToJson: unknown } | { matchesJsonPath: string } | { equalTo: string } | { matches: string };
export type Match = {
 method?: string; path?: string; pathRegex?: string;
 query?: Record<string, Matcher>; headers?: Record<string, Matcher>; body?: BodyMatcher[];
};
export type Response = { status: number; json?: unknown; body?: string; headers?: Record<string, string>; delayMs?: number };
export type Stub = Match & { response: Response; scenario?: { name: string; when: string; next?: string } };
function matchers(values: Record<string, Matcher> | undefined) {
 return values && Object.fromEntries(Object.entries(values).map(([key, value]) => [key, typeof value === 'string' ? { equalTo: value } : value]));
}
export class WireMock {
 constructor(readonly base = process.env.WIREMOCK_URL ?? 'http://127.0.0.1:18081') {}
 async admin<T = unknown>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const response = await fetch(`${this.base}/__admin${path}`, {
   method, headers: { 'Content-Type': 'application/json' },
   ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`WireMock ${method} ${path}: ${response.status} ${await response.text()}`);
  const text = await response.text(); return (text ? JSON.parse(text) : undefined) as T;
 }
 async reset() { await this.admin('/reset', 'POST'); }
 async resetHistory() { await this.admin('/requests', 'DELETE'); }
 async resetScenarios() { await this.admin('/scenarios/reset', 'POST'); }
 async resetMappings() { await this.admin('/mappings', 'DELETE'); }
 exchange(name: 'coinbase' | 'kraken' | 'kucoin') { return new Exchange(this, `/${name}`); }
 async snapshot() {
  const [requests, unmatched, mappings, scenarios] = await Promise.all([
   this.admin('/requests'), this.admin('/requests/unmatched'), this.admin('/mappings'), this.admin('/scenarios'),
  ]); return { requests, unmatched, mappings, scenarios };
 }
}
export class Exchange {
 constructor(private readonly wire: WireMock, private readonly prefix: string) {}
 private request(match: Match) {
  if (!!match.path === !!match.pathRegex) throw new Error('Specify exactly one of path or pathRegex');
  return {
   method: match.method ?? 'GET',
   ...(match.path ? { urlPath: this.prefix + match.path } : { urlPathPattern: this.prefix + match.pathRegex }),
   queryParameters: matchers(match.query), headers: matchers(match.headers), bodyPatterns: match.body,
  };
 }
 async stub(input: Stub) {
  const { response, scenario } = input;
  return this.wire.admin('/mappings', 'POST', {
   request: this.request(input),
   response: {
    status: response.status, jsonBody: response.json, body: response.body,
    headers: { ...(response.json === undefined ? {} : { 'Content-Type': 'application/json' }), ...response.headers },
    fixedDelayMilliseconds: response.delayMs,
   },
   ...(scenario ? { scenarioName: scenario.name, requiredScenarioState: scenario.when, newScenarioState: scenario.next } : {}),
  });
 }
 async sequence(match: Match, responses: Response[]) {
  if (!responses.length) throw new Error('Sequence needs a response');
  const name = randomUUID();
  for (const [index, response] of responses.entries()) {
   await this.stub({ ...match, response, scenario: {
    name, when: index === 0 ? 'Started' : `step-${index}`,
    ...(index < responses.length - 1 ? { next: `step-${index + 1}` } : {}),
   } });
  }
 }
 async count(match: Match) {
  const result = await this.wire.admin<{ count: number }>('/requests/count', 'POST', this.request(match)); return result.count;
 }
 async verify(match: Match, expected: number) {
  const count = await this.count(match);
  if (count !== expected) throw new Error(`Expected ${expected} matching ${this.prefix} requests, got ${count}: ${JSON.stringify(match)}`);
 }
}
export const wiremock = new WireMock();
export const coinbase = wiremock.exchange('coinbase');
export const kraken = wiremock.exchange('kraken');
export const kucoin = wiremock.exchange('kucoin');
export function tickerMatch(id: string): Match { return { method: 'GET', path: '/products/BTC-USD/ticker', headers: { 'X-Test-ID': id } }; }
