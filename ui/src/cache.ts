import type { Collection, Component, Installation, Manager, Overview, Runner, Stamp } from './schema.js';

export type Saved<T> = {
    data: T | null;
    observed: Stamp | null;
    age: string | null;
    anomaly: boolean;
    received: number;
    status: 'loading' | 'current' | 'stale' | 'missing' | 'unavailable';
    error: string | null;
};
export const empty = <T>(): Saved<T> => ({
    data: null,
    observed: null,
    age: null,
    anomaly: false,
    received: performance.now(),
    status: 'loading',
    error: null,
});
export function apply<T>(previous: Saved<T>, next: Component<T>, received: number): Saved<T> {
    if (next.availability === 'available')
        return {
            data: next.data,
            observed: next.observed_at,
            age: next.age_ns,
            anomaly: next.clock_anomaly,
            received,
            status: 'current',
            error: null,
        };
    if (next.cache_policy === 'retain' && previous.data !== null)
        return { ...previous, status: 'stale', error: next.error_code };
    return {
        ...empty<T>(),
        received,
        status: next.availability === 'missing' ? 'missing' : 'unavailable',
        error: next.error_code,
    };
}
export type Cache = {
    collection: Saved<Collection>;
    installation: Saved<Installation>;
    manager: Saved<Manager>;
    runner: Saved<Runner>;
    identity: string | null;
    service: Overview['service'] | null;
    header: Stamp | null;
};
export function initial(): Cache {
    return {
        collection: empty(),
        installation: empty(),
        manager: empty(),
        runner: empty(),
        identity: null,
        service: null,
        header: null,
    };
}
export function refresh(previous: Cache, next: Overview): Cache {
    const received = performance.now();
    const id = next.service.identity.installation_id;
    const discard = next.service.identity.cache_policy === 'discard' || id === null || id !== previous.identity;
    return {
        collection: apply(previous.collection, next.collection, received),
        installation: apply(discard ? empty() : previous.installation, next.service.installation, received),
        manager: apply(discard ? empty() : previous.manager, next.service.manager, received),
        runner: apply(discard ? empty() : previous.runner, next.service.runner, received),
        identity: id,
        service: next.service,
        header: next.header_at,
    };
}

export function age<T>(saved: Saved<T>, now: number): string {
    if (!saved.data) return '';
    if (saved.anomaly || saved.age === null) return 'Clock anomaly, age unknown';
    const seconds = (BigInt(saved.age) + BigInt(Math.max(0, Math.floor((now - saved.received) * 1e6)))) / 1000000000n;
    return `Observed ${seconds}s ago`;
}
