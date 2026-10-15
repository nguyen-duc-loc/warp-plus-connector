// The wire contract is closed. Values remain strings through validation and rendering.
type Schema<T> = (value: unknown) => T;
type Value<S> = S extends Schema<infer T> ? T : never;
export function invalid(): never { throw new Error('ui_protocol_invalid'); }
const text: Schema<string> = v => typeof v === 'string' && v.length <= 65536 ? v : invalid();
const bool: Schema<boolean> = v => typeof v === 'boolean' ? v : invalid();
const decimal: Schema<string> = v => typeof v === 'string' && /^(0|[1-9][0-9]*)$/.test(v) && v.length <= 128 ? v : invalid();
const signed: Schema<string> = v => typeof v === 'string' && /^(0|-?[1-9][0-9]*)$/.test(v) && v.length <= 128 ? v : invalid();
const one = <T extends string | number>(...values: T[]): Schema<T> => v => values.includes(v as T) ? v as T : invalid();
const nullable = <T>(schema: Schema<T>): Schema<T | null> => v => v === null ? null : schema(v);
const array = <T>(schema: Schema<T>, min: number, max: number): Schema<T[]> => v =>
  Array.isArray(v) && v.length >= min && v.length <= max ? v.map(schema) : invalid();
const object = <S extends Record<string, Schema<unknown>>>(shape: S): Schema<{[K in keyof S]: Value<S[K]>}> => v => {
  if (v === null || typeof v !== 'object' || Array.isArray(v)) invalid();
  const record = v as Record<string, unknown>;
  if (Object.keys(record).length !== Object.keys(shape).length || Object.keys(record).some(k => !(k in shape))) invalid();
  const out: Record<string, unknown> = {};
  for (const [key, schema] of Object.entries(shape)) out[key] = schema(record[key]);
  return out as {[K in keyof S]: Value<S[K]>};
};

// Generated from the Go diagnostic catalogue during the build, checked into source.
import {codes} from './codes.js';
const code: Schema<string> = v => typeof v === 'string' && codes.has(v) ? v : invalid();
const identity: Schema<string> = v => typeof v === 'string' && /^[a-f0-9]{32}$/.test(v) ? v : invalid();
const stamp = object({
  utc: (v: unknown) => typeof v === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z$/.test(v)
    && Number.isFinite(Date.parse(v)) && new Date(v).toISOString() === v.slice(0, 23) + 'Z'
    && !v.startsWith('0001-') ? v : invalid(),
  local: (v: unknown) => typeof v === 'string' && /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$/.test(v) ? v : invalid(),
  utc_offset_seconds: signed,
});
export type Stamp = Value<typeof stamp>;
const suggestion = object({action: one('sources', 'crawl'), command_text: text});
export const hello = object({release: text, commit: text, protocol: one(1), parent_pid: decimal,
  capabilities: array(one('overview'), 1, 1), color: one('auto', 'never'), refresh_delay_ns: decimal, timezone: text,
  profile: object({state_dir: text, config_path: nullable(text), suggestions: array(suggestion, 2, 2)})});
export type Hello = Value<typeof hello>;
const outcomes = object({unchecked: decimal, confirmed: decimal, invalid: decimal, inconclusive: decimal});
const counts = object({selected: decimal, pending: decimal, active: decimal, completed: decimal, skipped: decimal,
  interrupted: decimal, confirmed: decimal, invalid: decimal, inconclusive: decimal, unresolved: decimal, unstarted: decimal});
const run = object({id: decimal, kind: one('crawl', 'validation'), origin: one('manual', 'scheduled', 'unknown'),
  status: one('running', 'completed', 'failed', 'cancelled', 'interrupted'), started_at: stamp, finished_at: nullable(stamp),
  recovered_at: nullable(stamp), reason_code: nullable(code), clock_anomaly: bool,
  crawl_counts: nullable(object({saved_candidates: decimal, saved_sightings: decimal})), validation_counts: nullable(counts)});
const schedule = object({checkpoint_at: nullable(stamp), clock_regressed: bool, validation_recovery_pending: bool,
  crawl: nullable(object({coverage: array(object({channel: text, page_limit: decimal}), 1, 32), due_at: stamp,
    reason: one('initial', 'coverage_changed', 'completed', 'failed', 'cancelled', 'interrupted'),
    basis_at: stamp, interval_ns: decimal,
    basis_run_id: nullable(decimal), active_run_id: nullable(decimal)})),
  candidates: object({total: decimal, active: decimal, due: decimal, waiting: decimal, next_due_at: nullable(stamp)}),
  validation_wait: nullable(object({reason: one('backend_unavailable', 'cleanup_pending', 'provider_cooldown',
    'validator_busy', 'validator_clock_regressed', 'validation_rate_limited', 'validation_probe_failed',
    'validation_interrupted'), basis_at: stamp, retry_at: stamp, observed_at: stamp, interval_ns: decimal}))});
const collection = object({as_of: stamp, max_age_ns: decimal, total: decimal, latest_outcomes: outcomes,
  fresh_confirmed: decimal, stale_confirmed: decimal, unknown_freshness: decimal,
  recent_runs: array(run, 0, 10), older_runs_available: bool, schedule});
const installation = object({installation_id: identity, phase: text, autostart_enabled: nullable(bool), state_dir: text});
const manager = object({loaded: bool, running: nullable(bool), pid: nullable(decimal), last_exit_code: nullable(signed),
  last_signal: nullable(decimal), active_state: nullable(text), pending_job: nullable(bool), autostart_observed: nullable(bool)});
const runner = object({lock_held: nullable(bool), owner_pid: nullable(decimal), installation_matches: nullable(bool),
  stale: nullable(bool), clock_anomaly: bool, observation: nullable(object({
    phase: one('starting', 'running', 'clock_paused', 'stopping', 'stopped', 'failed'), started_at: stamp, updated_at: stamp,
    stopped_at: nullable(stamp), crawl_phase: one('unknown', 'disabled', 'idle', 'crawling', 'clock_paused', 'stopping'),
    validation_phase: one('unknown', 'disabled', 'idle', 'acquiring', 'checking', 'waiting', 'clock_paused', 'stopping'),
    failure_code: nullable(code)}))});
function component<T>(data: Schema<T>) {
  const parse = object({availability: one('available', 'missing', 'unavailable'), observed_at: stamp,
    age_ns: nullable(decimal), clock_anomaly: bool, cache_policy: one('replace', 'retain', 'discard'),
    error_code: nullable(code), data: nullable(data)});
  return (v: unknown) => {
    const c = parse(v);
    if (c.availability === 'available' ? c.data === null || c.error_code !== null || c.cache_policy !== 'replace'
      : c.data !== null || c.error_code === null || c.cache_policy === 'replace') invalid();
    if (c.availability === 'missing' && c.cache_policy !== 'discard') invalid();
    if (c.clock_anomaly !== (c.age_ns === null)) invalid();
    return c;
  };
}
const overviewShape = object({header_at: stamp, collection: component(collection), service: object({
  identity: object({state: one('verified', 'missing', 'changed', 'unknown'), installation_id: nullable(identity),
    cache_policy: one('retain', 'discard')}), collection_match: one('match', 'different', 'unknown'), bound_state_dir: nullable(text),
  installation: component(installation), manager: component(manager), runner: component(runner)})});
export function overview(v: unknown) {
  const o = overviewShape(v);
  const s = o.service;
  if (s.identity.state === 'verified') {
    if (s.identity.installation_id === null || s.bound_state_dir === null) invalid();
    if (s.installation.data && s.installation.data.installation_id !== s.identity.installation_id) invalid();
  } else if (s.identity.installation_id !== null || s.identity.cache_policy !== 'discard' ||
    s.collection_match !== 'unknown' || s.bound_state_dir !== null ||
    [s.installation, s.manager, s.runner].some(c => c.data !== null || c.cache_policy !== 'discard')) invalid();
  const c = o.collection.data;
  if (c && Object.values(c.latest_outcomes).reduce((a, n) => a + BigInt(n), 0n) !== BigInt(c.total)) invalid();
  return o;
}
export type Overview = ReturnType<typeof overview>;
export type Collection = Value<typeof collection>;
export type Component<T> = {availability: string; observed_at: Stamp; age_ns: string | null; clock_anomaly: boolean;
  cache_policy: string; error_code: string | null; data: T | null};
export type Installation = Value<typeof installation>;
export type Manager = Value<typeof manager>;
export type Runner = Value<typeof runner>;

export function reply(value: unknown, expected: string): unknown {
  if (!value || typeof value !== 'object' || Array.isArray(value)) invalid();
  const v = value as Record<string, unknown>;
  if (v.version !== 1 || v.id !== expected || Object.keys(v).length !== 3) invalid();
  if ('error' in v) { object({code})(v.error); invalid(); }
  if (!('result' in v)) invalid();
  return v.result;
}
