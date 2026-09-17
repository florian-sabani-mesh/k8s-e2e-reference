import {expect, test} from 'vitest';
import {createOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {coinbase, tickerMatch} from '../support/wiremock.js';

test('settles an order through real HTTP, Postgres, JetStream and Go consumers', async () => {
    const id = testId();
    const match = tickerMatch(id);
    await coinbase.stub({...match, response: {status: 200, json: {price: '67500.00'}}});
    const created = await createOrder(id);
    const order = await finalOrder(created.id);
    expect(order).toMatchObject({
        status: 'SETTLED',
        correlation_id: id,
        settlement_count: 1,
        settlement: {price: '67500.00000000', total: '6750.0000000000000000'}
    });
    expect(order.activity.map(event => event.kind)).toEqual(expect.arrayContaining(['PENDING', 'PRICE_RESOLVED', 'SETTLED']));
    await coinbase.verify(match, 1);
});
