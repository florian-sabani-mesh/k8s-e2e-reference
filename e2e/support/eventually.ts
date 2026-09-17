import { expect } from 'vitest';
import { getOrder, type Order } from './api.js';
export async function finalOrder(id: string, status: Order['status'] = 'SETTLED'): Promise<Order> {
 await expect.poll(() => getOrder(id), { timeout: 20_000, interval: 100, message: `Order ${id} should become ${status}` }).toMatchObject({ status });
 return getOrder(id);
}
