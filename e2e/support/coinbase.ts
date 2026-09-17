import { wiremock } from './wiremock.js';

// Coinbase App OAuth + v2 API stub helper. It hides raw WireMock JSON behind
// domain-shaped methods and models 2FA as a real header condition, not call order.
//
// These paths mirror the exchange service configuration (COINBASE_TOKEN_URL /
// COINBASE_API_BASE_URL pointed at WireMock). The client id/secret and callback are
// the deterministic E2E values Terraform injects.
export const coinbaseE2E = {
  clientId: 'e2e-coinbase-client-id',
  clientSecret: 'e2e-coinbase-client-secret',
  callbackUrl: 'http://gateway.e2e.svc.cluster.local:8080/exchanges/coinbase/callback',
  tokenPath: '/coinbase-oauth/oauth2/token',
  userPath: '/coinbase-api/v2/user',
  accountsPath: '/coinbase-api/v2/accounts',
} as const;

export type CoinbaseAccount = {
  id: string;
  asset: string;
  amount: string;
  name?: string;
  type?: string;
  primary?: boolean;
};

function sendPath(providerAccountId: string): string {
  return `/coinbase-api/v2/accounts/${providerAccountId}/transactions`;
}

async function stub(mapping: unknown): Promise<void> {
  await wiremock.admin('/mappings', 'POST', mapping);
}

async function count(requestPattern: unknown): Promise<number> {
  const result = await wiremock.admin<{ count: number }>('/requests/count', 'POST', requestPattern);
  return result.count;
}

export const coinbase = {
  oauth: {
    // Stubs the PKCE authorization-code token exchange, asserting form encoding and the
    // exact credentials, code and a present code_verifier.
    async stubTokenExchange(input: {
      code: string;
      accessToken: string;
      refreshToken: string;
      scope?: string;
    }): Promise<void> {
      await stub({
        priority: 1,
        request: {
          method: 'POST',
          urlPath: coinbaseE2E.tokenPath,
          headers: { 'Content-Type': { contains: 'application/x-www-form-urlencoded' } },
          bodyPatterns: [
            { contains: 'grant_type=authorization_code' },
            { contains: `code=${input.code}` },
            { contains: `client_id=${coinbaseE2E.clientId}` },
            { contains: `client_secret=${coinbaseE2E.clientSecret}` },
            { contains: 'redirect_uri=' },
            { matches: '.*code_verifier=[^&]+.*' },
          ],
        },
        response: {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
          jsonBody: {
            access_token: input.accessToken,
            token_type: 'bearer',
            expires_in: 3600,
            refresh_token: input.refreshToken,
            scope: input.scope ?? 'wallet:user:read,wallet:accounts:read,wallet:transactions:send,offline_access',
          },
        },
      });
    },
    // Stubs a token endpoint failure to exercise OAUTH_TOKEN_EXCHANGE_FAILED handling.
    async stubTokenFailure(status = 400): Promise<void> {
      await stub({
        request: { method: 'POST', urlPath: coinbaseE2E.tokenPath },
        response: { status, jsonBody: { error: 'invalid_grant' } },
      });
    },
  },
  api: {
    async stubUser(input: { id: string; name: string; nativeCurrency?: string; countryCode?: string }, accessToken = 'e2e-access-token'): Promise<void> {
      await stub({
        request: {
          method: 'GET',
          urlPath: coinbaseE2E.userPath,
          headers: { Authorization: { equalTo: `Bearer ${accessToken}` } },
        },
        response: {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
          jsonBody: {
            data: {
              id: input.id,
              name: input.name,
              native_currency: input.nativeCurrency ?? 'USD',
              country: { code: input.countryCode ?? 'US' },
            },
          },
        },
      });
    },
    async stubAccounts(accounts: CoinbaseAccount[], accessToken = 'e2e-access-token'): Promise<void> {
      await stub({
        request: {
          method: 'GET',
          urlPath: coinbaseE2E.accountsPath,
          headers: { Authorization: { equalTo: `Bearer ${accessToken}` } },
        },
        response: {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
          jsonBody: {
            pagination: { next_starting_after: null },
            data: accounts.map((a) => ({
              id: a.id,
              name: a.name ?? `${a.asset} Wallet`,
              primary: a.primary ?? false,
              type: a.type ?? (a.asset === 'USD' ? 'fiat' : 'wallet'),
              currency: { code: a.asset },
              balance: { amount: a.amount, currency: a.asset },
            })),
          },
        },
      });
    },
    async stubUserUnauthorized(): Promise<void> {
      await stub({
        request: { method: 'GET', urlPath: coinbaseE2E.userPath },
        response: { status: 401, jsonBody: { errors: [{ id: 'authentication_error', message: 'invalid token' }] } },
      });
    },
    // Models Coinbase Send Crypto with 2FA as a header condition: a request carrying the
    // correct CB-2FA-TOKEN succeeds; the same request without it demands 2FA (402). The
    // higher-priority success stub wins when the header is present.
    async require2FAForSend(input: { providerAccountId: string; validCode: string; transactionId?: string; status?: string }): Promise<void> {
      const path = sendPath(input.providerAccountId);
      await stub({
        priority: 1,
        request: {
          method: 'POST',
          urlPath: path,
          headers: { 'CB-2FA-TOKEN': { equalTo: input.validCode } },
        },
        response: {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
          jsonBody: {
            data: {
              id: input.transactionId ?? 'provider-transaction-123',
              type: 'send',
              status: input.status ?? 'pending',
              amount: { amount: '-0.25000000', currency: 'BTC' },
              native_amount: { amount: '-15000.00', currency: 'USD' },
              resource: 'transaction',
              network: { status: 'pending' },
            },
          },
        },
      });
      await stub({
        priority: 5,
        request: { method: 'POST', urlPath: path },
        response: {
          status: 402,
          headers: { 'Content-Type': 'application/json' },
          jsonBody: { errors: [{ id: 'two_factor_required', message: 'Two factor authentication required' }] },
        },
      });
    },
    // Unconditional successful send (no 2FA), for flows that do not exercise 2FA.
    async stubSend(input: { providerAccountId: string; transactionId?: string; status?: string }): Promise<void> {
      await stub({
        request: { method: 'POST', urlPath: sendPath(input.providerAccountId) },
        response: {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
          jsonBody: {
            data: {
              id: input.transactionId ?? 'provider-transaction-123',
              type: 'send',
              status: input.status ?? 'pending',
              amount: { amount: '-0.25000000', currency: 'BTC' },
              network: { status: 'pending' },
            },
          },
        },
      });
    },
  },
  async verifyTokenExchange(expected: number): Promise<void> {
    await expectCount({ method: 'POST', urlPath: coinbaseE2E.tokenPath }, expected, 'token exchange');
  },
  async verifyUser(expected: number): Promise<void> {
    await expectCount({ method: 'GET', urlPath: coinbaseE2E.userPath }, expected, 'user');
  },
  async verifyAccounts(expected: number): Promise<void> {
    await expectCount({ method: 'GET', urlPath: coinbaseE2E.accountsPath }, expected, 'accounts');
  },
  async verifySendCount(input: { providerAccountId: string; count: number }): Promise<void> {
    await expectCount({ method: 'POST', urlPath: sendPath(input.providerAccountId) }, input.count, `send to ${input.providerAccountId}`);
  },
  async verifyNoSend(providerAccountId: string): Promise<void> {
    await expectCount({ method: 'POST', urlPath: sendPath(providerAccountId) }, 0, `send to ${providerAccountId}`);
  },
  async sendRequests(providerAccountId: string): Promise<CoinbaseSendJournalEntry[]> {
    // /requests/find returns bare LoggedRequest objects (headers/body at the top level).
    const result = await wiremock.admin<{ requests: Array<{ headers?: Record<string, string | string[]>; body?: string }> }>(
      '/requests/find',
      'POST',
      { method: 'POST', urlPath: sendPath(providerAccountId) },
    );
    return result.requests.map((entry) => ({
      headers: lowerKeys(entry.headers ?? {}),
      body: entry.body ? (JSON.parse(entry.body) as Record<string, unknown>) : {},
    }));
  },
};

export type CoinbaseSendJournalEntry = { headers: Record<string, string>; body: Record<string, unknown> };

function lowerKeys(headers: Record<string, string | string[]>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(headers).map(([key, value]) => [key.toLowerCase(), Array.isArray(value) ? (value[0] ?? '') : value]),
  );
}

async function expectCount(requestPattern: unknown, expected: number, label: string): Promise<void> {
  const actual = await count(requestPattern);
  if (actual !== expected) {
    throw new Error(`Expected ${expected} Coinbase ${label} request(s), got ${actual}`);
  }
}
