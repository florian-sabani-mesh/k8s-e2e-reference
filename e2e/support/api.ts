export type OrderInput = {
    asset: string;
    currency: string;
    amount: string;
    exchange: 'coinbase' | 'kraken' | 'kucoin'
};
export type Order = OrderInput & {
    id: string; correlation_id: string; status: 'PENDING' | 'PRICE_RESOLVED' | 'SETTLED' | 'FAILED';
    error_code: string | null; settlement_count: number;
    settlement: { id: string; price: string; total: string } | null;
    activity: Array<{ event_id: string; kind: string }>;
};
export const apiURL = process.env.API_URL ?? 'http://127.0.0.1:18080';
export const btcOrder: OrderInput = {asset: 'BTC', currency: 'USD', amount: '0.1', exchange: 'coinbase'};

async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(`${apiURL}${path}`, {...options, signal: AbortSignal.timeout(5_000)});
    if (!response.ok) throw new Error(`${path}: HTTP ${response.status}: ${await response.text()}`);
    return await response.json() as T;
}

export function createOrder(correlation: string, input: OrderInput = btcOrder) {
    return request<{ id: string; status: string }>('/orders', {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'X-Test-ID': correlation},
        body: JSON.stringify(input),
    });
}

export function getOrder(id: string) {
    return request<Order>(`/orders/${id}`);
}
