import {coinbase, type CoinbaseAccount} from './coinbase.js';

// Black-box client for the exchange APIs. Every call goes through the gateway, the
// real external boundary, exactly as a browser or client would.
export const gatewayURL = process.env.API_URL ?? 'http://127.0.0.1:18080';

export type ExchangeResponse<T = any> = {
    status: number;
    body: T;
    location: string | null;
};

function idHeaders(correlationId?: string): Record<string, string> {
    return correlationId ? {'X-Test-ID': correlationId} : {};
}

async function call<T = any>(path: string, init?: RequestInit): Promise<ExchangeResponse<T>> {
    // redirect: 'manual' lets us inspect the OAuth callback's 302 without a browser.
    const response = await fetch(`${gatewayURL}${path}`, {
        ...init,
        redirect: 'manual',
        signal: AbortSignal.timeout(10_000)
    });
    const text = await response.text();
    let body: unknown = undefined;
    if (text) {
        try {
            body = JSON.parse(text);
        } catch {
            body = text;
        }
    }
    return {status: response.status, body: body as T, location: response.headers.get('location')};
}

export function createExchangeUrl(exchange: string, accountId: string, redirectUrl: string, correlationId?: string): Promise<ExchangeResponse> {
    const query = new URLSearchParams({accountId, redirectUrl});
    return call(`/exchanges/${exchange}/url?${query.toString()}`, {headers: idHeaders(correlationId)});
}

export function exchangeCallback(exchange: string, params: {
    code: string;
    state: string
}, correlationId?: string): Promise<ExchangeResponse> {
    const query = new URLSearchParams(params);
    return call(`/exchanges/${exchange}/callback?${query.toString()}`, {headers: idHeaders(correlationId)});
}

export function getExchangeBalance(exchange: string, accountId: string, correlationId?: string, quote = 'USD'): Promise<ExchangeResponse> {
    return call(`/exchanges/${exchange}/accounts/${accountId}/balance?quote=${quote}`, {headers: idHeaders(correlationId)});
}

export type WithdrawalPayload = {
    idem: string;
    asset: string;
    amount: string;
    to: string;
    network: string;
    twoFactorCode?: string;
};

export function createWithdrawal(exchange: string, accountId: string, payload: WithdrawalPayload, correlationId?: string): Promise<ExchangeResponse> {
    return call(`/exchanges/${exchange}/accounts/${accountId}/withdrawals`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', ...idHeaders(correlationId)},
        body: JSON.stringify(payload),
    });
}

export const defaultRedirectUrl = 'http://localhost:3000/settings/exchanges';

export const defaultCoinbaseAccounts: CoinbaseAccount[] = [
    {id: 'btc-account', asset: 'BTC', amount: '1.5', primary: true},
    {id: 'eth-account', asset: 'ETH', amount: '2'},
    {id: 'usd-account', asset: 'USD', amount: '1000'},
];

// connectCoinbase performs the entire real OAuth flow through the running system: it
// requests an authorization URL, stubs the Coinbase token/user/accounts endpoints, then
// drives the callback so a real Go pod exchanges the code and persists the connection.
// Nothing is inserted into the database directly.
export async function connectCoinbase(
    accountId: string,
    options: { correlationId?: string; accounts?: CoinbaseAccount[]; authCode?: string } = {},
): Promise<{ state: string; authorizationUrl: string; callback: ExchangeResponse }> {
    const correlationId = options.correlationId;
    const accounts = options.accounts ?? defaultCoinbaseAccounts;
    const authCode = options.authCode ?? 'e2e-auth-code';

    const urlResponse = await createExchangeUrl('coinbase', accountId, defaultRedirectUrl, correlationId);
    if (urlResponse.status !== 200) {
        throw new Error(`createExchangeUrl failed: ${urlResponse.status} ${JSON.stringify(urlResponse.body)}`);
    }
    const state = urlResponse.body.state as string;

    await coinbase.oauth.stubTokenExchange({
        code: authCode,
        accessToken: 'e2e-access-token',
        refreshToken: 'e2e-refresh-token'
    });
    await coinbase.api.stubUser({
        id: 'coinbase-user-123',
        name: 'E2E Coinbase User',
        nativeCurrency: 'USD',
        countryCode: 'US'
    });
    await coinbase.api.stubAccounts(accounts);

    const callback = await exchangeCallback('coinbase', {code: authCode, state}, correlationId);
    if (callback.status !== 302) {
        throw new Error(`callback did not redirect: ${callback.status} ${JSON.stringify(callback.body)}`);
    }
    return {state, authorizationUrl: urlResponse.body.authorizationUrl as string, callback};
}
