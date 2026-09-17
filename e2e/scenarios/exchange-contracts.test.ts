import {expect, test} from 'vitest';
import {btcOrder, createOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {kraken, kucoin} from '../support/wiremock.js';

test('uses the Kraken query contract and parses its nested ticker', async () => {
    const id = testId();
    const match = {
        pathRegex: '/0/public/.*',
        query: {pair: 'BTCUSD'},
        headers: {'X-Test-ID': id, Accept: 'application/json'}
    };
    await kraken.stub({
        ...match,
        response: {status: 200, json: {error: [], result: {XXBTZUSD: {c: ['67500.00', '1']}}}}
    });
    const order = await finalOrder((await createOrder(id, {...btcOrder, exchange: 'kraken'})).id);
    expect(order.settlement_count).toBe(1);
    await kraken.verify(match, 1);
});
test('uses the KuCoin query contract and checks its success code', async () => {
    const id = testId();
    const match = {path: '/api/v1/market/orderbook/level1', query: {symbol: 'BTC-USD'}, headers: {'X-Test-ID': id}};
    await kucoin.stub({...match, response: {status: 200, json: {code: '200000', data: {price: '67500.00'}}}});
    const order = await finalOrder((await createOrder(id, {...btcOrder, exchange: 'kucoin'})).id);
    expect(order.settlement_count).toBe(1);
    await kucoin.verify(match, 1);
});
