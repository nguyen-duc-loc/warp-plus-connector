import test from 'node:test';
import assert from 'node:assert/strict';
import {job, operation, OperationError, filters, view, resultPage, detailPage, revealed, receipt} from './results-schema.js';

test('ordinary job error fields are metadata, not operation error envelopes',()=>{
  const running={job_id:'9007199254740993',kind:'open',state:'running',phase:'collection',completed_count:'1',
    expected_count:null,error_code:null,warning_code:null,remaining_path:null,result:null};
  assert.equal(operation(running,job).job_id,'9007199254740993');
  assert.throws(()=>operation({error_code:'ui_busy'},job),OperationError);
  for(const bad of [{...running,extra:true},{...running,job_id:'9223372036854775808'},
    {...running,warning_code:'ui_snapshot_cleanup_failed'},{...running,result:{type:'view',value:{}}},
    {...running,state:'succeeded'},{...running,state:'failed'}])assert.throws(()=>job(bad));
});

const at = {utc:'2026-10-15T00:00:00.000000Z',local:'2026-10-15 00:00:00',utc_offset_seconds:'0'};
const candidate = {id:'9007199254740993',first_seen_at:at,last_seen_at:at,latest_outcome:'unchecked',
  latest_reason:null,last_definitive:null,evidence_at:null,freshness:'not_applicable',clock_anomaly:false,
  latest_attempt_id:null,definitive_attempt_id:null,active_attempt_id:null};
const savedView = {snapshot_id:'1',view_id:'1',as_of:at,max_age_ns:'1',
  filters:{status:[],definitive:[],freshness:[]},matching_count:'2',marked_count:'0',removed_marks:'0',
  focus_id:candidate.id,focus_offset:'0',retained:false,error_code:null};

// covers: spec 0011 AC-1, AC-12, AC-13. Ordinals and IDs stay exact across page boundaries.
test('result pages reject gaps, duplicates, reversed IDs and private fields',()=>{
  const row = {summary:candidate,marked:false};
  const page = {view:savedView,offset:'0',items:[row],next_offset:'1'};
  assert.equal(resultPage(page).items[0]!.summary.id,'9007199254740993');
  assert.equal(resultPage({...page,offset:'1',next_offset:null}).next_offset,null);
  for(const bad of [
    {...page,next_offset:'2'}, {...page,items:[]}, {...page,offset:'2'},
    {...page,items:[row,row],next_offset:null},
    {...page,items:[row,{...row,summary:{...candidate,id:'9007199254740992'}}],next_offset:null},
    {...page,items:[{...row,summary:{...candidate,key:'private'}}]},
    {...page,items:[{...row,summary:{...candidate,id:9007199254740992}}]},
    {...page,items:Array(51).fill(row)},
  ]) assert.throws(()=>resultPage(bad),/ui_protocol_invalid/);
});

// covers: spec 0011 AC-4, AC-13. History records retain unavailable facts and bounded display text.
test('detail history preserves absent retry facts and rejects inconsistent evidence',()=>{
  const display = {text:'fixture',shortened:false};
  const attempt = {id:'1',candidate_id:candidate.id,run_id:'2',backend_id:display,backend_version:display,
    started_at:at,finished_at:null,recovered_at:null,outcome:null,reason_code:null,evidence:null,
    clock_anomaly:false,retry:null};
  const page = {detail_id:'1',section:'attempts',offset:'0',total:'1',items:[attempt],next_offset:null};
  const parsed = detailPage(page).items[0]!;
  assert.ok('retry' in parsed);
  assert.equal(parsed.retry,null);
  for(const bad of [
    {...attempt,retry:{attempt_number:'1',failure_class:null,wait_ns:'-1',eligible_at:null}},
    {...attempt,evidence:{kind:'account_type',observed_at:at,account_type:display,rejection_code:display}},
    {...attempt,backend_version:{text:'é'.repeat(2049),shortened:true}},
    {...attempt,request_id:'private'},
  ]) assert.throws(()=>detailPage({...page,items:[bad]}),/ui_protocol_invalid/);
});

// covers: spec 0011 AC-5, AC-13. Reveal limits count encoded bytes, including quotes and escapes.
test('reveal accepts exact keys only within its encoded byte budget',()=>{
  const value = {view_id:'1',detail_id:'1',candidate_id:candidate.id,key:'x'.repeat(256*1024-2)};
  assert.equal(revealed(value).key,value.key);
  assert.throws(()=>revealed({...value,key:value.key+'x'}),/ui_protocol_invalid/);
  assert.throws(()=>revealed({...value,key:'\u0000'.repeat(45000)}),/ui_protocol_invalid/);
  assert.throws(()=>revealed({...value,request_token:'private'}),/ui_protocol_invalid/);
});

// covers: spec 0011 AC-9, AC-15. Publication and durability cannot contradict one another.
test('export receipts distinguish absence, publication and unknown outcomes',()=>{
  const value = {destination:'/fixture/result',selection:'matching',format:'json',include_keys:false,
    selected_count:'9007199254740993',prepared_count:'9007199254740993',published:true,
    durability:'confirmed',error_code:null,remaining_path:null};
  assert.equal(receipt(value).selected_count,'9007199254740993');
  assert.equal(receipt({...value,published:null,durability:'uncertain'}).published,null);
  assert.equal(receipt({...value,published:false,durability:'not_published'}).published,false);
  for(const bad of [
    {...value,prepared_count:'9007199254740994'},
    {...value,published:false}, {...value,published:null}, {...value,durability:'not_published'},
    {...value,error_code:'private failure text'}, {...value,key:'private'},
  ]) assert.throws(()=>receipt(bad),/ui_protocol_invalid/);
});

test('canonical filters and retained metadata cannot silently change meaning',()=>{
  assert.throws(()=>filters({status:['invalid','unchecked'],definitive:[],freshness:[]}));
  assert.throws(()=>filters({status:['unchecked','unchecked'],definitive:[],freshness:[]}));
  const stamp={utc:'2026-10-15T00:00:00.000000Z',local:'2026-10-15 00:00:00',utc_offset_seconds:'0'};
  const valid={snapshot_id:'1',view_id:'1',as_of:stamp,max_age_ns:'1',filters:{status:[],definitive:[],freshness:[]},
    matching_count:'338',marked_count:'1',removed_marks:'0',focus_id:'338',focus_offset:'337',retained:true,
    error_code:'state_not_initialized'};
  assert.equal(view(valid).focus_id,'338');
  assert.throws(()=>view({...valid,error_code:null}));
  assert.throws(()=>view({...valid,marked_count:'339'}));
  assert.throws(()=>view({...valid,focus_offset:'338'}));
  assert.throws(()=>view({...valid,focus_id:null}));
});
