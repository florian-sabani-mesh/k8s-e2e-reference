# k8s-e2e-reference

> **Mock the world you don't control. Run everything you do control.**

This repository is a reference for end-to-end testing a real microservice system the way it actually runs in production — on Kubernetes, talking to a real database over real networking — while faking **only** the one thing you can't run yourself: a third‑party API (Coinbase).

Everything else is real. The Go services run as containers inside a real Kubernetes cluster, discover each other through real DNS, make real HTTP and database calls, and persist to a real PostgreSQL. The only stand‑in is [WireMock](https://wiremock.org/), which plays the role of Coinbase so that no test ever moves real money or depends on the internet.

The payoff: one command spins the whole world up, runs the tests, and tears it down.

```bash
make e2e
```

## What's real, and what's faked

| Real | Faked |
| --- | --- |
| Go microservices, built as Docker images | Coinbase (OAuth, accounts, Send Crypto) |
| A real Kubernetes cluster (via kind) | *…that's it* |
| Kubernetes DNS and service‑to‑service HTTP | |
| PostgreSQL, with real migrations | |
| NATS JetStream, with durable consumers | |
| OAuth2 + PKCE, token encryption, retries, health probes | |

Because the third‑party is the only fake, a passing test tells you the *real* wiring works: routing, service discovery, database transactions, OAuth, idempotency, and error handling.

## Why kind and Kubernetes?

**Kubernetes** is how these services run in production — as containers that find each other by name and talk over the network. **kind** ("Kubernetes IN Docker") runs a genuine Kubernetes cluster entirely inside Docker on your laptop. No cloud account, no credentials, no external infrastructure — just Docker.

Testing on real Kubernetes (instead of, say, running the code in-process or with mocked HTTP) means the test exercises the same boundaries production does: a request crosses the network, hits a pod, opens a database transaction, and calls out to an upstream. If any of that is wrong, the test catches it — because it's the real thing.

## Getting started

### Install the prerequisites

Start Docker, then install these tools:

| Tool | Version |
| --- | --- |
| Docker (with BuildKit) | 29.x |
| kind | 0.33.0 |
| kubectl | 1.35.0 |
| Terraform | 1.14.7 |
| Go | 1.27.1 |
| Node.js / npm | 24 |
| make, bash, curl | system tools |

On macOS: `brew install kind kubectl terraform go node`. Give Docker roughly 4 CPUs, 6 GB RAM, and 10 GB of disk.

### Run it

```bash
git clone https://github.com/florian-sabani-mesh/k8s-e2e-reference
cd k8s-e2e-reference
make e2e
```

`make e2e` creates a kind cluster, builds and loads the service images, deploys everything with Terraform, runs database migrations, waits for readiness, runs the test suite, and cleans up — collecting diagnostics first if anything fails.

Useful variations:

```bash
make e2e                    # the whole lifecycle, then tear down
KEEP_CLUSTER=1 make e2e     # leave the cluster running to poke at it
make destroy                # tear down a kept cluster
make help                   # everything else
```

## A sample test: a Coinbase withdrawal that needs 2FA

This test connects a Coinbase account and then withdraws crypto from it. Coinbase requires two‑factor authentication before it will send money, so the first attempt is challenged and the client retries with a code. **The whole thing runs through the live system** — only Coinbase's HTTP responses are canned.

The version below is deliberately spelled out. In the real suite these Coinbase mocks are tucked behind small helpers (`connectCoinbase(...)`, `coinbase.api.*`), but here they're inlined so you can see *exactly* which Coinbase requests are faked and what they return. Each canned response is a WireMock mapping: a **request to match** and the **response to send back**. Everything that is *not* a WireMock mapping is your real system doing real work.

```ts
test('withdrawal requires 2FA, replays with the same idem, then is idempotent', async () => {
  const accountId = testId(); // our app's own internal account id (a UUID)

  // ── 1. Ask OUR service for a Coinbase authorization URL ────────────────────────
  // Real call: gateway → exchange service. The service invents a random `state`, a PKCE
  // verifier/challenge, saves an OAuth session row in Postgres, and hands back the URL.
  const { body: url } = await createExchangeUrl('coinbase', accountId, 'http://localhost:3000/done');
  // url.authorizationUrl = https://login.coinbase.com/oauth2/auth?...&state=<random>&code_challenge=...

  // ── 2. The user opens that URL, logs into Coinbase, and approves access ─────────
  // This happens in a real browser on Coinbase's own site — entirely outside our system.
  // There is nothing for us to run here. Coinbase would now redirect the browser back to
  // our callback with a one-time `code` and the same `state`.

  // ── 3. Script the Coinbase responses our callback is about to trigger ───────────
  // These are the ONLY faked parts of the whole flow. WireMock plays Coinbase.

  // (3a) exchange the authorization code for OAuth tokens
  wiremock.stub({
    request:  { method: 'POST', urlPath: '/coinbase-oauth/oauth2/token' },
    response: { status: 200, jsonBody: {
      access_token: 'e2e-access-token', refresh_token: 'e2e-refresh-token',
      token_type: 'bearer', expires_in: 3600,
    } },
  });
  // (3b) who is the connected Coinbase user?
  wiremock.stub({
    request:  { method: 'GET', urlPath: '/coinbase-api/v2/user',
                headers: { Authorization: { equalTo: 'Bearer e2e-access-token' } } },
    response: { status: 200, jsonBody: { data: {
      id: 'coinbase-user-123', name: 'E2E Coinbase User', native_currency: 'USD',
    } } },
  });
  // (3c) which wallets/balances does the user have?
  wiremock.stub({
    request:  { method: 'GET', urlPath: '/coinbase-api/v2/accounts',
                headers: { Authorization: { equalTo: 'Bearer e2e-access-token' } } },
    response: { status: 200, jsonBody: { data: [
      { id: 'btc-account', currency: { code: 'BTC' }, balance: { amount: '1.5',  currency: 'BTC' } },
      { id: 'usd-account', currency: { code: 'USD' }, balance: { amount: '1000', currency: 'USD' } },
    ] } },
  });

  // ── 4. Simulate Coinbase redirecting the browser back to OUR callback ───────────
  // Real call to our /callback. Under the hood the exchange service now, for real:
  //   • looks up the OAuth session by `state` (single-use) and rejects any tampering,
  //   • POSTs the code to Coinbase's token endpoint    (real HTTP → mapping 3a),
  //   • GETs the user, then the accounts               (real HTTP → mappings 3b, 3c),
  //   • encrypts the tokens and stores the connection + balances in Postgres.
  await exchangeCallback('coinbase', { code: 'e2e-auth-code', state: url.state });
  // The Coinbase account is now connected. Everything above was real except Coinbase's replies.

  // ── 5. Script Coinbase's 2FA rule for sending crypto ────────────────────────────
  // A rule, not a fixed script: a send WITH the correct 2FA header succeeds; the very
  // same send WITHOUT it is challenged. (WireMock picks the higher-priority match.)
  wiremock.stub({ // WITH the 2FA token → 201 pending
    priority: 1,
    request:  { method: 'POST', urlPath: '/coinbase-api/v2/accounts/btc-account/transactions',
                headers: { 'CB-2FA-TOKEN': { equalTo: '123456' } } },
    response: { status: 201, jsonBody: { data: {
      id: 'provider-tx-1', type: 'send', status: 'pending', network: { status: 'pending' },
    } } },
  });
  wiremock.stub({ // WITHOUT it → 402 two_factor_required
    priority: 5,
    request:  { method: 'POST', urlPath: '/coinbase-api/v2/accounts/btc-account/transactions' },
    response: { status: 402, jsonBody: {
      errors: [{ id: 'two_factor_required', message: 'Two factor authentication required' }],
    } },
  });

  const idem = testId(); // the idempotency key the client chooses for THIS withdrawal
  const base = { idem, asset: 'BTC', amount: '0.25', to: SAMPLE_BTC_ADDRESS, network: 'bitcoin' };

  // ── 6. First withdrawal attempt, no 2FA code ────────────────────────────────────
  // Under the hood: gateway → exchange service, which for real:
  //   • fetches the connected Coinbase account from Postgres to confirm it exists,
  //   • checks the requested 0.25 BTC against the stored 1.5 BTC balance (it passes),
  //   • records the attempt keyed by `idem`,
  //   • sends the request to Coinbase (real HTTP → hits the 402 mapping, no token yet).
  const first = await createWithdrawal('coinbase', accountId, base);
  expect(first.status).toBe(402);
  expect(first.body.error.code).toBe('TWO_FACTOR_REQUIRED');

  // ── 7. Retry with the SAME idem key, now with the 2FA code ──────────────────────
  // The service replays the identical request and adds the CB-2FA-TOKEN header, so this
  // time it hits the 201 mapping. It records the provider transaction as *pending*.
  const second = await createWithdrawal('coinbase', accountId, { ...base, twoFactorCode: '123456' });
  expect(second.status).toBe(201);
  expect(second.body.status).toBe('PROVIDER_PENDING');

  // ── 8. Verify what Coinbase actually received ───────────────────────────────────
  // Two identical sends reached Coinbase; only the second carried the 2FA token. Reading
  // WireMock's request journal is fair game — a third-party contract is observable.
  const sends = await coinbase.sendRequests('btc-account');
  expect(sends).toHaveLength(2);
  expect(sends.filter((s) => s.headers['cb-2fa-token'] === '123456')).toHaveLength(1);
});
```

Notice what was *not* faked: the gateway, the Go exchange service, the OAuth handshake, the PostgreSQL reads and writes, the balance check, the idempotency logic, and every hop of Kubernetes networking. The only scripted things are the five WireMock mappings standing in for Coinbase's replies.

## How this actually works

Want the full picture — why WireMock is a network service rather than a Node mock, why we mock only the exchange, how the 2FA replay and idempotency are proven, and how "as close to production as possible" is achieved?

👉 **[OAUTH_COINBASE_2FA_E2E_TESTING.md](./OAUTH_COINBASE_2FA_E2E_TESTING.md)** — a conceptual walkthrough of the approach.

## Troubleshooting

- **Docker not running:** start Docker Desktop/Engine and check `docker info`.
- **Something failed mid‑run:** re‑run with `KEEP_CLUSTER=1 make e2e`, then explore with `kubectl` (`export KUBECONFIG="$PWD/.run/kubeconfig"`). `make diagnostics` saves logs, events, and WireMock's request history to `artifacts/`.
- **Stuck cluster:** `make destroy`.

This is a **testing reference**, not a production trading deployment — one replica per service, a throwaway database password, no funds, no ingress. It exists to show a way of testing, faithfully, without the world.
