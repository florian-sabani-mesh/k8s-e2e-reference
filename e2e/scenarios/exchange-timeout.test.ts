import {expect, test} from 'vitest';
import {createOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {coinbase, tickerMatch} from '../support/wiremock.js';

test('exhausts real HTTP deadlines and exposes a failure without settlement', async () => {
    const id = testId();
    const match = tickerMatch(id);
    await coinbase.stub({...match, response: {status: 200, json: {price: '67500.00'}, delayMs: 1500}});
    const created = await createOrder(id);
    const order = await finalOrder(created.id, 'FAILED');
    expect(order).toMatchObject({error_code: 'UPSTREAM_TIMEOUT', settlement_count: 0, settlement: null});
    // WireMock completes journal entries after their delayed response; wait for that observable.
    await expect.poll(() => coinbase.count(match), {timeout: 5_000, interval: 100}).toBe(3);
});
