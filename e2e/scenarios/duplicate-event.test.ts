import {expect, test} from 'vitest';
import {createOrder, getOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {coinbase, tickerMatch} from '../support/wiremock.js';
import {natsControl} from '../support/nats.js';

test('processes two created events but commits exactly one business settlement', async () => {
    const id = testId();
    const match = tickerMatch(id);
    await coinbase.stub({...match, response: {status: 200, json: {price: '67500.00'}}});
    const created = await createOrder(id);
    const first = await finalOrder(created.id);
    const control = await natsControl();
    try {
        const duplicateID = await control.duplicateCreated(first);
        await expect.poll(() => getOrder(first.id), {timeout: 15_000, interval: 100}).toMatchObject({
            activity: expect.arrayContaining([{event_id: `${duplicateID}:price`, kind: 'DUPLICATE_IGNORED'}]),
        });
        const after = await getOrder(first.id);
        expect(after.status).toBe('SETTLED');
        expect(after.settlement_count).toBe(1);
        expect(after.settlement).toEqual(first.settlement);
        await coinbase.verify(match, 2);
    } finally {
        await control.close();
    }
});
