import {expect, test} from 'vitest';
import {createOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {coinbase, tickerMatch} from '../support/wiremock.js';

for (const status of [503, 429]) {
    test(`retries real HTTP ${status} twice then settles with exactly three requests`, async () => {
        const id = testId();
        const match = tickerMatch(id);
        await coinbase.sequence(match, [
            {status, headers: {'Retry-After': '0'}},
            {status, headers: {'Retry-After': '0'}},
            {status: 200, json: {price: '67500.00'}},
        ]);
        const created = await createOrder(id);
        const order = await finalOrder(created.id);
        expect(order.settlement_count).toBe(1);
        await coinbase.verify(match, 3);
    });
}
test('does not retry a permanent HTTP error', async () => {
    const id = testId();
    const match = tickerMatch(id);
    await coinbase.stub({...match, response: {status: 400, json: {message: 'unsupported pair'}}});
    const created = await createOrder(id);
    const order = await finalOrder(created.id, 'FAILED');
    expect(order).toMatchObject({error_code: 'UPSTREAM_STATUS', settlement_count: 0, settlement: null});
    await coinbase.verify(match, 1);
});
