import {expect, test} from 'vitest';
import {createOrder} from '../support/api.js';
import {finalOrder} from '../support/eventually.js';
import {testId} from '../support/ids.js';
import {coinbase, tickerMatch} from '../support/wiremock.js';
import {natsControl} from '../support/nats.js';
import {isReady, kubectl, pods} from '../support/kubernetes.js';

// Scenario 6: prove that work survives losing the only settlement consumer.
//
// We force-delete the settlement pod and then submit an order. Gateway, orders and
// pricing stay up, so the order is priced and `price.resolved` is published while no
// settlement consumer is online. JetStream must retain that message durably until
// Kubernetes schedules a replacement pod (selected purely by label, never by a name we
// knew in advance). The replacement re-binds the durable consumer, drains the backlog
// and settles exactly once — no lost message, no duplicated business effect.
//
// The freeze-the-process approach is deliberately avoided: a container's PID 1 is the
// namespace init, and the kernel drops an unhandled SIGSTOP to it (which is why
// `docker pause` uses the cgroup freezer, not a signal), so it cannot deterministically
// hold a delivery unacknowledged.
test('recovers a queued delivery after Kubernetes replaces the settlement pod', async () => {
    const id = testId();
    const match = tickerMatch(id);
    await coinbase.stub({...match, response: {status: 200, json: {price: '67500.00'}}});
    const control = await natsControl();
    try {
        const old = (await pods('settlement')).find(isReady);
        if (!old) throw new Error('No ready settlement pod');
        // Abruptly remove the only consumer, simulating a crash rather than a graceful drain.
        await kubectl('delete', 'pod', old.metadata.name, '--grace-period=0', '--force=true', '--wait=false');
        // Submit work while settlement is offline: the price is resolved and queued with no
        // consumer to receive it, exercising JetStream's durable retention.
        const created = await createOrder(id);
        // Kubernetes recreates the pod under a fresh identity; we find it only via its label.
        await expect
            .poll(
                async () => (await pods('settlement')).some((pod) => pod.metadata.uid !== old.metadata.uid && isReady(pod)),
                {timeout: 60_000, interval: 200, message: 'Kubernetes should schedule a replacement settlement pod'},
            )
            .toBe(true);
        // The replacement consumer drains the backlog and settles the order exactly once.
        const order = await finalOrder(created.id);
        expect(order).toMatchObject({
            status: 'SETTLED',
            settlement_count: 1,
            settlement: {total: '6750.0000000000000000'}
        });
        expect(order.activity.filter((event) => event.kind === 'SETTLED')).toHaveLength(1);
        await coinbase.verify(match, 1);
        // The durable consumer ends clean: the delivery was acknowledged, nothing left in flight.
        await expect
            .poll(async () => (await control.consumer('settlement-v1')).num_ack_pending, {
                timeout: 10_000,
                interval: 100
            })
            .toBe(0);
    } finally {
        await control.close();
    }
});
