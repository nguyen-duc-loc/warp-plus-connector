import {array, bool, code, invalid, nullable, object, one, stamp, text, type Schema, type Value} from './schema.js';

const count: Schema<string> = v => typeof v === 'string' && /^(0|[1-9][0-9]*)$/.test(v)
  && v.length <= 19 && BigInt(v) <= 9223372036854775807n ? v : invalid();
const id: Schema<string> = v => count(v) !== '0' ? v as string : invalid();
const canonical = (order: string[]): Schema<string[]> => v => {
  if (!Array.isArray(v) || v.some((x,i) => typeof x !== 'string' || !order.includes(x)
    || (i > 0 && order.indexOf(v[i-1]) >= order.indexOf(x)))) invalid();
  return v as string[];
};
export const filterValues = {status: ['unchecked','confirmed','invalid','inconclusive'],
  definitive: ['none','confirmed','invalid'], freshness: ['fresh','stale','unknown','not_applicable']};
export const filters = object({status: canonical(filterValues.status), definitive: canonical(filterValues.definitive),
  freshness: canonical(filterValues.freshness)});
export type Filters = Value<typeof filters>;
export const emptyFilters = (): Filters => ({status: [], definitive: [], freshness: []});
export const summary = object({id, first_seen_at: stamp, last_seen_at: stamp,
  latest_outcome: one('unchecked','confirmed','invalid','inconclusive'), latest_reason: nullable(code),
  last_definitive: nullable(one('confirmed','invalid')), evidence_at: nullable(stamp),
  freshness: one('fresh','stale','unknown','not_applicable'), clock_anomaly: bool,
  latest_attempt_id: nullable(id), definitive_attempt_id: nullable(id), active_attempt_id: nullable(id)});
export type Summary = Value<typeof summary>;
const viewShape = object({snapshot_id: id, view_id: id, as_of: stamp, max_age_ns: id, filters,
  matching_count: count, marked_count: count, removed_marks: count, focus_id: nullable(id), focus_offset: nullable(count),
  retained: bool, error_code: nullable(code)});
export const view = (v: unknown) => {
  const r = viewShape(v);
  if (BigInt(r.marked_count) > BigInt(r.matching_count) || (r.focus_id === null) !== (r.focus_offset === null)
    || (r.matching_count === '0') !== (r.focus_id === null)
    || (r.focus_offset !== null && BigInt(r.focus_offset) >= BigInt(r.matching_count))
    || r.retained !== (r.error_code !== null)) invalid();
  return r;
};
export type View = ReturnType<typeof view>;
const row = object({summary, marked: bool});
export type Row = Value<typeof row>;
const pageShape = object({view, offset: count, items: array(row,0,50), next_offset: nullable(count)});
function pageBounds(offset: string, length: number, total: string, next: string | null) {
  const end = BigInt(offset) + BigInt(length);
  if (end > BigInt(total) || (end === BigInt(total) ? next !== null : next !== end.toString() || length === 0)) invalid();
}
export function resultPage(v: unknown) {
  const p = pageShape(v);pageBounds(p.offset,p.items.length,p.view.matching_count,p.next_offset);
  if (p.items.some((r,i) => i > 0 && BigInt(r.summary.id) <= BigInt(p.items[i-1]!.summary.id))) invalid();
  return p;
}
export type ResultPage = ReturnType<typeof resultPage>;
export const marks = object({view_id: id, candidate_id: id, marked: bool, marked_count: count});
export const detail = object({detail_id: id, view_id: id, candidate_id: id, as_of: stamp, summary,
  source_count: count, attempt_count: count});
export type Detail = Value<typeof detail>;
const displayShape = object({text, shortened: bool});
const display = (v: unknown) => {
  const d = displayShape(v);if (Buffer.byteLength(d.text) > 4096) invalid();return d;
};
const source = object({id,candidate_id:id,source_id:display,message_id:display,message_url:nullable(display),
  published_at:nullable(stamp),first_seen_at:stamp,last_seen_at:stamp});
const evidenceShape = object({kind:one('account_type','rejection'),observed_at:stamp,
  account_type:nullable(display),rejection_code:nullable(display)});
const evidence = (v: unknown) => {
  const e = evidenceShape(v);
  if (e.kind === 'account_type' ? e.account_type === null || e.rejection_code !== null
    : e.rejection_code === null || e.account_type !== null) invalid();return e;
};
const retry = object({attempt_number:id,failure_class:nullable(one('temporary','throttled','terminal','unsafe')),
  wait_ns:count,eligible_at:nullable(stamp)});
const attempt = object({id,candidate_id:id,run_id:id,backend_id:display,backend_version:display,
  started_at:stamp,finished_at:nullable(stamp),recovered_at:nullable(stamp),
  outcome:nullable(one('confirmed','invalid','inconclusive')),reason_code:nullable(code),evidence:nullable(evidence),
  clock_anomaly:bool,retry:nullable(retry)});
export type SourceRow = Value<typeof source>;
export type AttemptRow = Value<typeof attempt>;
export function detailPage(v: unknown) {
  const p = object({detail_id:id,section:one('sources','attempts'),offset:count,total:count,
    items:(v: unknown) => Array.isArray(v) && v.length <= 50 ? v : invalid(),next_offset:nullable(count)})(v);
  const items = p.section === 'sources' ? p.items.map(source) : p.items.map(attempt);
  pageBounds(p.offset,items.length,p.total,p.next_offset);
  if (items.some((r,i) => i > 0 && BigInt(r.id) <= BigInt(items[i-1]!.id))) invalid();
  return {...p,items};
}
export type DetailPage = ReturnType<typeof detailPage>;
const revealKey: Schema<string> = v => typeof v === 'string' && Buffer.byteLength(JSON.stringify(v)) <= 256*1024 ? v : invalid();
export const revealed = object({view_id:id,detail_id:id,candidate_id:id,key:revealKey});
const selection = one('matching','marked');const format = one('json','keys');
export const review = object({review_id:id,snapshot_id:id,view_id:id,selection,selected_count:count,
  matching_count:count,filters,as_of:stamp,max_age_ns:id,retained:bool,format,include_keys:bool,destination:text});
export type Review = Value<typeof review>;
const receiptShape = object({destination:text,selection,format,include_keys:bool,selected_count:count,
  prepared_count:count,published:nullable(bool),durability:one('not_published','confirmed','uncertain'),
  error_code:nullable(code),remaining_path:nullable(text)});
export function receipt(v: unknown) {
  const r = receiptShape(v);
  if (BigInt(r.prepared_count)>BigInt(r.selected_count) || (r.published === false && r.durability !== 'not_published')
    || (r.published !== false && r.durability === 'not_published')
    || (r.durability === 'confirmed' && r.published !== true)) invalid();return r;
}
export type Receipt = ReturnType<typeof receipt>;
type JobResult = {type:'view';value:View} | {type:'detail';value:Detail} | {type:'review';value:Review}
  | {type:'export';value:Receipt};
function jobResult(v: unknown): JobResult {
  const r = object({type:one('view','detail','review','export'),value:(v:unknown)=>v})(v);
  switch (r.type) {
    case 'view': return {type:r.type,value:view(r.value)};
    case 'detail': return {type:r.type,value:detail(r.value)};
    case 'review': return {type:r.type,value:review(r.value)};
    case 'export': return {type:r.type,value:receipt(r.value)};
  }
}
export const started = object({job_id:id});
const jobShape = object({job_id:id,kind:one('open','refresh','filter','clear_marks','details','review','export'),
  state:one('running','cancelling','succeeded','failed','cancelled'),
  phase:one('collection','view','summary','sources','attempts','destination','encoding','publication','cleanup','complete'),
  completed_count:count,expected_count:nullable(count),error_code:nullable(code),
  warning_code:nullable(one('ui_snapshot_cleanup_failed')),remaining_path:nullable(text),result:nullable(jobResult)});
export function job(v: unknown) {
  const j = jobShape(v);const active = j.state === 'running' || j.state === 'cancelling';
  if (active && (j.result !== null || j.error_code !== null || j.warning_code !== null)) invalid();
  if (j.state === 'succeeded' && (j.error_code !== null || j.result === null || j.phase !== 'complete')) invalid();
  if ((j.state === 'failed' || j.state === 'cancelled') && (j.error_code === null || j.phase === 'complete'
    || (j.result !== null && j.kind !== 'export'))) invalid();
  if (j.warning_code !== null && (j.state !== 'succeeded' || j.kind === 'export' || j.remaining_path === null)) invalid();
  if (j.result !== null && j.result.type !== (['open','refresh','filter','clear_marks'].includes(j.kind) ? 'view'
    : j.kind === 'details' ? 'detail' : j.kind)) invalid();
  return j;
}
export type Job = ReturnType<typeof job>;
export const empty = object({});
export function operation<T>(value: unknown, parse: Schema<T>): T {
  if (value !== null && typeof value === 'object' && 'error_code' in value && Object.keys(value).length === 1) {
    const error = object({error_code:code})(value);throw new OperationError(error.error_code);
  }
  return parse(value);
}
export class OperationError extends Error {}
