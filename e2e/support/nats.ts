import { connect } from '@nats-io/transport-node';
import { jetstream, jetstreamManager } from '@nats-io/jetstream';
import { randomUUID } from 'node:crypto';
import type { Order } from './api.js';
export async function natsControl() {
 const nc = await connect({ servers: process.env.NATS_URL ?? 'nats://127.0.0.1:14222', timeout: 3_000 });
 const js = jetstream(nc); const manager = await jetstreamManager(nc);
 return {
  close: () => nc.drain(),
  consumer: (name: string) => manager.consumers.info('ORDERS', name),
  duplicateCreated: async (order: Order) => {
   // Distinct transport ID deliberately bypasses JetStream's finite dedup window.
   // Same business order must still settle only once.
   const id = randomUUID();
   const event = { version: 1, id, order_id: order.id, correlation_id: order.correlation_id, asset: order.asset, currency: order.currency, amount: order.amount, exchange: order.exchange };
   await js.publish('orders.created', new TextEncoder().encode(JSON.stringify(event)), { msgID: id });
   return id;
  },
 };
}
