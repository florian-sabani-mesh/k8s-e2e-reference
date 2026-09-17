import {expect, test} from 'vitest';
import {createOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {coinbase, tickerMatch} from '../support/wiremock.js';

for (const body of ['{broken JSON', '{"price":"not-a-number"}', '{"unexpected":true}']) {
    test(`rejects malformed price (${body}) and continues processing valid orders`, async () => {
        const id = testId();
        const match = tickerMatch(id);
        await coinbase.stub({...match, response: {status: 200, body, headers: {'Content-Type': 'application/json'}}});
        const created = await createOrder(id);
        const failed = await finalOrder(created.id, 'FAILED');
        expect(failed).toMatchObject({error_code: 'MALFORMED_RESPONSE', settlement_count: 0, settlement: null});
        await coinbase.verify(match, 1);
        // Prove the worker still makes progress; a health endpoint alone is weaker evidence.
        const next = testId();
        await coinbase.stub({...tickerMatch(next), response: {status: 200, json: {price: '67500.00'}}});
        await finalOrder((await createOrder(next)).id);
        await coinbase.verify(tickerMatch(next), 1);
    });
}
