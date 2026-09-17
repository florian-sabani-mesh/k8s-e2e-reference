import { expect, test } from 'vitest';
import { testId } from '../support/ids.js';
import { coinbase as ticker } from '../support/wiremock.js';
import { connectCoinbase, getExchangeBalance } from '../support/exchange.js';

// Scenario E: USD portfolio valuation. After a real OAuth connection, the exchange
// service prices each asset through the pricing service over real Kubernetes HTTP
// (exchange -> pricing -> WireMock), then sums exact decimal values.
//
//   1.5 BTC @ $60,000 = $90,000
//   2   ETH @ $3,000  = $6,000
//   1000 USD @ $1     = $1,000
//   ----------------------------
//   TOTAL             = $97,000
test('values a connected Coinbase portfolio in USD via the pricing service', async () => {
  const accountId = testId();
  await connectCoinbase(accountId, { correlationId: testId() });

  // Deterministic spot prices served to the pricing service's Coinbase ticker path.
  await ticker.stub({ method: 'GET', path: '/products/BTC-USD/ticker', response: { status: 200, json: { price: '60000.00' } } });
  await ticker.stub({ method: 'GET', path: '/products/ETH-USD/ticker', response: { status: 200, json: { price: '3000.00' } } });

  const response = await getExchangeBalance('coinbase', accountId, testId(), 'USD');
  expect(response.status).toBe(200);
  expect(response.body).toMatchObject({ accountId, exchange: 'coinbase', quoteCurrency: 'USD', total: '97000.00' });
  expect(typeof response.body.asOf).toBe('string');

  const byAsset = Object.fromEntries((response.body.assets as any[]).map((a) => [a.asset, a]));
  expect(byAsset.BTC).toMatchObject({
    providerAccountId: 'btc-account',
    amount: '1.50000000',
    price: { amount: '60000.00', currency: 'USD' },
    value: { amount: '90000.00', currency: 'USD' },
  });
  expect(byAsset.ETH).toMatchObject({
    providerAccountId: 'eth-account',
    amount: '2.00000000',
    price: { amount: '3000.00', currency: 'USD' },
    value: { amount: '6000.00', currency: 'USD' },
  });
  // USD is valued at 1 without an external price lookup.
  expect(byAsset.USD).toMatchObject({
    providerAccountId: 'usd-account',
    amount: '1000.00',
    price: { amount: '1.00', currency: 'USD' },
    value: { amount: '1000.00', currency: 'USD' },
  });
});

// A balance request for an account that never connected returns a normalized 404.
test('returns EXCHANGE_CONNECTION_NOT_FOUND for an unconnected account', async () => {
  const response = await getExchangeBalance('coinbase', testId(), testId());
  expect(response.status).toBe(404);
  expect(response.body.error.code).toBe('EXCHANGE_CONNECTION_NOT_FOUND');
});
