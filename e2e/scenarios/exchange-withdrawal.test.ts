import {expect, test} from 'vitest';
import {testId} from '../support/ids.js';
import {coinbase} from '../support/coinbase.js';
import {connectCoinbase, createWithdrawal} from '../support/exchange.js';

const SAMPLE_BTC_ADDRESS = '1AUJ8z5RuHkTPQ1eikyfUUetzGmdwLGkpT';

// Scenario F (mandatory): a withdrawal exceeding the stored balance is rejected by our
// own local risk check, before any money-moving Coinbase call is made.
test('rejects an over-balance withdrawal locally without calling Coinbase', async () => {
    const accountId = testId();
    await connectCoinbase(accountId, {correlationId: testId()});
    // Even with a working send stub in place, the send must not be reached.
    await coinbase.api.require2FAForSend({providerAccountId: 'btc-account', validCode: '123456'});

    const response = await createWithdrawal(
        'coinbase',
        accountId,
        {idem: testId(), asset: 'BTC', amount: '10', to: SAMPLE_BTC_ADDRESS, network: 'bitcoin'},
        testId(),
    );
    expect(response.status).toBe(422);
    expect(response.body.error.code).toBe('INSUFFICIENT_BALANCE');
    await coinbase.verifyNoSend('btc-account');
});

// Scenarios G, H, I, J: the full 2FA replay and idempotency contract in one WireMock
// session so the Send Crypto request counts are unambiguous.
test('withdrawal requires 2FA, replays with the same idem, then is idempotent', async () => {
    const accountId = testId();
    await connectCoinbase(accountId, {correlationId: testId()});
    await coinbase.api.require2FAForSend({
        providerAccountId: 'btc-account',
        validCode: '123456',
        transactionId: 'provider-tx-1',
        status: 'pending',
    });

    const idem = testId();
    const base = {idem, asset: 'BTC', amount: '0.25', to: SAMPLE_BTC_ADDRESS, network: 'bitcoin'};

    // G: first attempt without a 2FA code -> Coinbase 402 -> our TWO_FACTOR_REQUIRED.
    const first = await createWithdrawal('coinbase', accountId, base, testId());
    expect(first.status).toBe(402);
    expect(first.body.error).toMatchObject({code: 'TWO_FACTOR_REQUIRED', retryable: true, idem});
    await coinbase.verifySendCount({providerAccountId: 'btc-account', count: 1});

    // H: same idem + 2FA code -> Coinbase 201 pending -> PROVIDER_PENDING.
    const second = await createWithdrawal('coinbase', accountId, {...base, twoFactorCode: '123456'}, testId());
    expect(second.status).toBe(201);
    expect(second.body).toMatchObject({
        idem,
        exchange: 'coinbase',
        asset: 'BTC',
        amount: '0.25000000',
        status: 'PROVIDER_PENDING',
        providerTransactionId: 'provider-tx-1',
    });
    await coinbase.verifySendCount({providerAccountId: 'btc-account', count: 2});

    // Both provider sends carried identical business fields; only the second had the token.
    const sends = await coinbase.sendRequests('btc-account');
    expect(sends).toHaveLength(2);
    for (const send of sends) {
        expect(send.body).toMatchObject({
            type: 'send',
            to: SAMPLE_BTC_ADDRESS,
            amount: '0.25',
            currency: 'BTC',
            idem,
            network: 'bitcoin'
        });
    }
    expect(sends.filter((s) => s.headers['cb-2fa-token'] === '123456')).toHaveLength(1);
    expect(sends.filter((s) => s.headers['cb-2fa-token'] === undefined)).toHaveLength(1);

    // I: replay the completed idem -> stored result, no third Coinbase call.
    const replay = await createWithdrawal('coinbase', accountId, {...base, twoFactorCode: '123456'}, testId());
    expect(replay.status).toBe(200);
    expect(replay.body).toMatchObject({status: 'PROVIDER_PENDING', providerTransactionId: 'provider-tx-1'});
    await coinbase.verifySendCount({providerAccountId: 'btc-account', count: 2});

    // J: same idem, changed amount -> 409 conflict, still no new Coinbase call.
    const conflict = await createWithdrawal('coinbase', accountId, {
        ...base,
        amount: '0.50',
        twoFactorCode: '123456'
    }, testId());
    expect(conflict.status).toBe(409);
    expect(conflict.body.error.code).toBe('IDEMPOTENCY_CONFLICT');
    await coinbase.verifySendCount({providerAccountId: 'btc-account', count: 2});
});

// Input validation and unknown-asset handling, none of which call Coinbase.
test('validates withdrawal input and unknown assets', async () => {
    const accountId = testId();
    await connectCoinbase(accountId, {correlationId: testId()});
    await coinbase.api.require2FAForSend({providerAccountId: 'btc-account', validCode: '123456'});

    const badIdem = await createWithdrawal(
        'coinbase',
        accountId,
        {idem: 'not-a-uuid', asset: 'BTC', amount: '0.1', to: SAMPLE_BTC_ADDRESS, network: 'bitcoin'},
        testId(),
    );
    expect(badIdem.status).toBe(400);
    expect(badIdem.body.error.code).toBe('INVALID_REQUEST');

    const negative = await createWithdrawal(
        'coinbase',
        accountId,
        {idem: testId(), asset: 'BTC', amount: '-1', to: SAMPLE_BTC_ADDRESS, network: 'bitcoin'},
        testId(),
    );
    expect(negative.status).toBe(400);

    const unknownAsset = await createWithdrawal(
        'coinbase',
        accountId,
        {idem: testId(), asset: 'DOGE', amount: '1', to: SAMPLE_BTC_ADDRESS, network: 'dogecoin'},
        testId(),
    );
    expect(unknownAsset.status).toBe(404);
    expect(unknownAsset.body.error.code).toBe('ASSET_ACCOUNT_NOT_FOUND');

    await coinbase.verifyNoSend('btc-account');
});
