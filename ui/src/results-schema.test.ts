import test from 'node:test';
import assert from 'node:assert/strict';
import {job, operation, OperationError, filters, view} from './results-schema.js';

test('ordinary job error fields are metadata, not operation error envelopes',()=>{
  const running={job_id:'9007199254740993',kind:'open',state:'running',phase:'collection',completed_count:'1',
    expected_count:null,error_code:null,warning_code:null,remaining_path:null,result:null};
  assert.equal(operation(running,job).job_id,'9007199254740993');
  assert.throws(()=>operation({error_code:'ui_busy'},job),OperationError);
  for(const bad of [{...running,extra:true},{...running,job_id:'9223372036854775808'},
    {...running,warning_code:'ui_snapshot_cleanup_failed'},{...running,result:{type:'view',value:{}}},
    {...running,state:'succeeded'},{...running,state:'failed'}])assert.throws(()=>job(bad));
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
