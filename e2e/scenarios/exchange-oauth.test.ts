import { expect, test } from 'vitest';
import { testId } from '../support/ids.js';
import { coinbase } from '../support/coinbase.js';
import { connectCoinbase, createExchangeUrl, defaultRedirectUrl, exchangeCallback } from '../support/exchange.js';

// Scenario A: the OAuth authorization URL is generated with PKCE and random state that
// is bound server-side to our internal accountId.
test('generates a Coinbase OAuth URL with PKCE and unguessable state', async () => {
  const accountId = testId();
  const response = await createExchangeUrl('coinbase', accountId, defaultRedirectUrl, testId());
  expect(response.status).toBe(200);
  expect(response.body).toMatchObject({ exchange: 'coinbase', accountId, redirectUrl: defaultRedirectUrl });
  expect(typeof response.body.state).toBe('string');
  // State must be random, not the accountId, and non-trivial.
  expect(response.body.state).not.toBe(accountId);
  expect(response.body.state.length).toBeGreaterThanOrEqual(20);

  const url = new URL(response.body.authorizationUrl);
  expect(url.searchParams.get('response_type')).toBe('code');
  expect(url.searchParams.get('client_id')).toBeTruthy();
  expect(url.searchParams.get('redirect_uri')).toBeTruthy();
  expect(url.searchParams.get('state')).toBe(response.body.state);
  expect(url.searchParams.get('code_challenge')).toBeTruthy();
  expect(url.searchParams.get('code_challenge_method')).toBe('S256');
  const scope = url.searchParams.get('scope') ?? '';
  expect(scope).toContain('wallet:transactions:send');
  // The authorize redirect_uri is our callback, never the application redirectUrl.
  expect(url.searchParams.get('redirect_uri')).not.toBe(defaultRedirectUrl);
});

// Scenario B: the callback performs real HTTP to Coinbase (WireMock) and persists.
test('completes the OAuth callback with real token, user and accounts requests', async () => {
  const accountId = testId();
  const correlationId = testId();
  const { callback } = await connectCoinbase(accountId, { correlationId });

  expect(callback.status).toBe(302);
  expect(callback.location).toContain(defaultRedirectUrl);
  expect(callback.location).toContain('status=connected');
  // Tokens must never appear in the redirect.
  expect(callback.location ?? '').not.toContain('access');
  expect(callback.location ?? '').not.toContain('token');

  // Prove a real Go pod made real HTTP calls to the fake Coinbase network boundary.
  await coinbase.verifyTokenExchange(1);
  await coinbase.verifyUser(1);
  await coinbase.verifyAccounts(1);
});

// Scenario C: a callback with the wrong state is rejected and never exchanges a token.
test('rejects a callback with an unknown OAuth state and never calls Coinbase', async () => {
  const accountId = testId();
  await createExchangeUrl('coinbase', accountId, defaultRedirectUrl, testId());
  // Stub the token endpoint so a mistaken call would succeed — proving it is not called.
  await coinbase.oauth.stubTokenExchange({ code: 'e2e-auth-code', accessToken: 'e2e-access-token', refreshToken: 'e2e-refresh-token' });

  const response = await exchangeCallback('coinbase', { code: 'e2e-auth-code', state: `${testId()}-not-real` }, testId());
  expect(response.status).toBe(400);
  expect(response.body.error.code).toBe('INVALID_OAUTH_STATE');
  await coinbase.verifyTokenExchange(0);
});

// Scenario D: OAuth state is single-use; replaying the same code+state is rejected and
// does not exchange a second token.
test('rejects OAuth state replay after a successful callback', async () => {
  const accountId = testId();
  const { state } = await connectCoinbase(accountId, { correlationId: testId() });
  await coinbase.verifyTokenExchange(1);

  const replay = await exchangeCallback('coinbase', { code: 'e2e-auth-code', state }, testId());
  expect(replay.status).toBe(409);
  expect(replay.body.error.code).toBe('OAUTH_STATE_ALREADY_USED');
  // Still exactly one token exchange: the replay never reached Coinbase.
  await coinbase.verifyTokenExchange(1);
});

// Edge cases: input validation and unsupported providers, none of which call Coinbase.
test('validates inputs and rejects unsupported exchanges', async () => {
  const accountId = testId();

  const badAccount = await createExchangeUrl('coinbase', 'not-a-uuid', defaultRedirectUrl);
  expect(badAccount.status).toBe(400);
  expect(badAccount.body.error.code).toBe('INVALID_ACCOUNT_ID');

  const badRedirect = await createExchangeUrl('coinbase', accountId, 'http://evil.example.com/callback');
  expect(badRedirect.status).toBe(400);
  expect(badRedirect.body.error.code).toBe('INVALID_REDIRECT_URL');

  const unsupported = await createExchangeUrl('kraken', accountId, defaultRedirectUrl);
  expect(unsupported.status).toBe(400);
  expect(unsupported.body.error.code).toBe('UNSUPPORTED_EXCHANGE');
});

// The token endpoint failing surfaces a normalized OAUTH_TOKEN_EXCHANGE_FAILED error.
test('surfaces a normalized error when the Coinbase token endpoint fails', async () => {
  const accountId = testId();
  const response = await createExchangeUrl('coinbase', accountId, defaultRedirectUrl, testId());
  const state = response.body.state as string;
  await coinbase.oauth.stubTokenFailure(400);

  const callback = await exchangeCallback('coinbase', { code: 'e2e-auth-code', state }, testId());
  expect(callback.status).toBe(502);
  expect(callback.body.error.code).toBe('OAUTH_TOKEN_EXCHANGE_FAILED');
});
