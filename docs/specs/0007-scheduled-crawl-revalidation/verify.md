# Scheduled crawl and revalidation verification

Verified on 2026-10-11 against the current working tree. The production CLI and controlled application, store, and validator paths passed on macOS arm64 and Linux arm64. All 15 criteria are met. The Beta test workflow remains next.

You can repeat timing and fault checks with controlled adapters and the real coordinator and SQLite store. The probes use injected clocks, timers, driver faults, and private journal faults. Production intervals remain unchanged.

| Scenario | Observable result | Criteria |
| --- | --- | --- |
| [x] Fresh state, one source, one candidate | One scheduled crawl and check survive restart; inspection explains the next due time | AC-1, AC-3, AC-4, AC-11, AC-12, AC-13 |
| [x] Every invalid flag and configuration boundary | Failure precedes state creation or external work; disabled lane flags preserve explicit false | AC-1 |
| [x] Validation disabled, then crawling disabled | Each independent lane operates; both disabled fails; saved eligibility survives reenabling | AC-1, AC-7 |
| [x] Slow check, retry wait, and next crawl deadline | Crawl requests complete while validation remains active; no second crawl or batch overlaps its own lane | AC-2 |
| [x] Worker and mutation observations | At most configured workers execute; command spacing and one candidate attempt invariants hold | AC-2, AC-10 |
| [x] 338 due candidates and repeated bounded selections | Every candidate is reached in deterministic order with no collection truncation; each selection is within its fixed bound | AC-3, AC-15 |
| [x] New discoveries during a held batch | They become due immediately and enter selection after that immutable batch ends | AC-3 |
| [x] Confirmation, invalidity, exhausted retries, rediscovery | Routine and failure intervals use terminal facts; evidence freshness does not move on rediscovery or inconclusive results | AC-4 |
| [x] Downtime spanning many intervals | One due crawl and bounded candidate selection occur, with no replayed missed occurrences | AC-5 |
| [x] Manual exact, subset, changed, and reordered coverage | Only the exact saved full plan advances that schedule; changed unsatisfied coverage is due immediately | AC-6 |
| [x] Interval changes and repeated restarts | Due times recompute from unchanged bases; restart itself does not perpetually postpone work | AC-5, AC-7 |
| [x] Backward UTC while idle, active, and across restart | New external admission pauses, cleanup remains possible, deadlines and evidence remain intact | AC-7, AC-14 |
| [x] Forward UTC and resume after clock repair | Overdue work is coalesced and one probe at most is admitted | AC-7, AC-9 |
| [x] Backend unavailable, then available | Crawls continue, retries are paced, no fake candidate attempts precede successful acquisition | AC-8 |
| [x] Empty profile starts during validator contention or a shared clock wait | Saved recovery obligation survives restart and triggers inspection after the wait, without requiring a candidate or creating a probe | AC-5, AC-8, AC-10 |
| [x] Repeated restart before a saved backend or cleanup deadline | No early acquisition occurs and the failure basis does not move; a configured interval change recomputes from that same basis | AC-5, AC-7, AC-8 |
| [x] Docker accepts a connection but never responds, with no candidate | One acquisition and recovery attempt uses a single five minute budget across phases, retains its obligation, then waits the failure interval | AC-8, AC-14 |
| [x] Cleanup uncertain, then proven | No registration occurs before proof; retry waits survive restart and crawling continues | AC-8 |
| [x] Permanent identity, permissions, image, or journal fault | Explicit fatal diagnostic and bounded stop, with no automatic mutation loop | AC-8, AC-10, AC-14 |
| [x] Profile A throttles, profile B attempts work | Both manual and scheduled checks respect the original shared deadline; a shorter interval cannot bypass it | AC-9 |
| [x] Cooldown expires with several due candidates | One saved probe uses one worker and attempt; normal batches resume only after healthy response, cleanup, and commit | AC-9 |
| [x] Probe fails, is cancelled, or crashes | New cooldown or retained recovery state prevents immediate repeated admission | AC-9, AC-14 |
| [x] Manual multiple ID check during required probe | Complete ID validation remains intact; command returns probe required without a partial run | AC-9 |
| [x] `validator recover` during cooldown | Cleanup can finish but the provider restriction remains | AC-8, AC-9 |
| [x] Two runners, manual writer, and concurrent reader | Losing writers perform no external work; listing, export, and schedule inspection remain usable | AC-10 |
| [x] Inspection while another process commits | All counts and references in one response use one snapshot; keys and raw provider text are absent | AC-11 |
| [x] Writer commits a checkpoint immediately around inspection startup | Snapshot establishment precedes the fixed clock sample; a normal commit cannot cause a false clock regression warning | AC-7, AC-11 |
| [x] Missing, empty, old, and damaged state | No read creates or repairs state; output has the defined empty or error semantics | AC-11, AC-13 |
| [x] Stdout or stderr failure and SIGINT or SIGTERM | No repeated writes to a failed stream; cleanup and committed history survive with the defined exit status | AC-12, AC-14 |
| [x] Remote cleanup ends at 40 seconds or consumes the 60 second allowance | Shared local finalization deadlines are respectively stop plus 45 or 65 seconds, with no new writes after the applicable deadline | AC-14 |
| [x] Schema migration and recovery fault injection | Version, eligibility, references, and existing history remain atomic | AC-13 |
| [x] Registry versions 1, 2, 3, and unknown newer version | Safe conversion requires proven cleanup; older executables reject version 3; unsupported versions fail closed | AC-9, AC-13 |
| [x] Crash between journal admission, result commit, and gate clearing | Extra waiting is possible; extra unguarded admission is not | AC-9, AC-13, AC-14 |
| [x] Profile B recovers profile A while A's database is unavailable | Proven cleanup creates a cooldown with the explicit interrupted or probe failure reason; B never finalizes A's run, and opening still requires a new healthy committed probe | AC-5, AC-9, AC-13 |

## Acceptance evidence

| Criterion | Verdict | Observation | Evidence |
| --- | --- | --- | --- |
| AC-1 | Met | Startup boundaries reject invalid settings before state access. Explicit false overrides JSON. Both lanes operate independently. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume/final-startup.json) |
| AC-2 | Met | Slow validation and retry waits permit crawling. Workers stay within policy. The gap also persists across batch boundaries. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume/fixed-batch.jsonl) |
| AC-3 | Met | All 338 candidates are selected in order in 85 bounded batches. A discovery during a held batch waits for the next immutable selection. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume/fixed-batch.jsonl) |
| AC-4 | Met | Confirmation and invalidity use the routine interval. Exhausted attempts use the failure interval. Rediscovery and inconclusive checks preserve definitive evidence. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume/fixed-batch.jsonl) |
| AC-5 | Met | Seven process kill boundaries preserve completed work and recover only started work. Repeated restarts preserve timing bases. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume-state/evidence.json) |
| AC-6 | Met | Exact manual coverage advances the schedule. A subset does not. Reordered coverage becomes due. Ordinary source failure gets a future deadline. | [Runtime evidence](/private/tmp/wpc-verify-0007/controlled.jsonl) |
| AC-7 | Met | Idle and active backward clocks pause admission. Repair resumes work and forward jumps coalesce it. Restart and policy edits retain evidence times. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume-clock/fixed-macos.log) |
| AC-8 | Met | Backend, contention, clock, and cleanup waits survive restart. Acquisition has one five minute budget. Permanent journal errors stop after one attempt. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume/final-recovery.jsonl) |
| AC-9 | Met | Both profiles respect the original cooldown. One acknowledged healthy probe restores normal capacity. Abandoned phases and older executables cannot bypass the gate. | [Runtime evidence](/private/tmp/wpc-verify-0007-journal/evidence.json) |
| AC-10 | Met | Competing writers report busy while reads and export work. Lost SQL acknowledgement stops further SQL across both lanes while remote cleanup remains possible. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume-state/final-report.json) |
| AC-11 | Met | Inspection uses a fixed snapshot and safe candidate facts. Missing, empty, old, damaged, and unknown records have defined behavior. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume-state/final-report.json) |
| AC-12 | Met | Terminal summaries and diagnostics stay separate. Broken streams stop the runner. Fatal SQL uncertainty retains state_error instead of becoming cancelled. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume-state/final-report.json) |
| AC-13 | Met | Schema migration and recovery commit atomically. Journal conversion waits for cleanup. Unknown and older versions fail closed. | [Runtime evidence](/private/tmp/wpc-verify-0007-journal/evidence.json) |
| AC-14 | Met | Shared cleanup at 40 or 60 seconds yields local deadlines at 45 or 65 seconds. Entered writes retain ownership past a deadline and no new writes start afterward. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume-clock/evidence.json) |
| AC-15 | Met | Controlled runtime checks pass on both arm64 platforms. Both builds and the repository gate pass. No real provider calls were made. | [Runtime evidence](/private/tmp/wpc-verify-0007-resume/debug-all-gate.log) |

## Commands and artifacts

You can inspect the following records for exact commands, outputs, exit codes, and injected fault boundaries.

1. [Final macOS CLI run](/private/tmp/wpc-verify-0007-final-runtime/runtime.log) records a real Telegram crawl saving 100 candidates, schema version 5, saved inspection, competing writers, and signal exits 130 and 143. [Final Linux CLI run](/private/tmp/wpc-verify-0007-resume/final-linux-cli.log) records source failure, continued running, saved inspection, contention, and SIGTERM.
2. [macOS startup cases](/private/tmp/wpc-verify-0007-resume/final-startup.json) and [Linux startup cases](/private/tmp/wpc-verify-0007-resume/final-linux-startup.json) record 29 cases each. [Reader and precedence checks](/private/tmp/wpc-verify-0007-resume/final-reader-and-precedence.json) include export while the runner owns the writer.
3. [macOS batch driver](/private/tmp/wpc-verify-0007-resume/fixed-batch.jsonl) and [Linux batch driver](/private/tmp/wpc-verify-0007-resume/fixed-linux-batch.jsonl) run `go run ./batchprobe` and its compiled Linux equivalent. Both show ordered selection, worker bounds, mutation spacing, retry overlap, and invalid outcomes.
4. [Integrated gate checks](/private/tmp/wpc-verify-0007-resume/fixed-linux-integrated.jsonl) exercise manual ID validation, copied cooldowns, a saved probe with one worker and attempt, and restoration of normal batches. [Journal records](/private/tmp/wpc-verify-0007-journal/evidence.json) include six lost acknowledgement boundaries and historical revision `a736390` rejecting registry version 3.
5. [Clock and shutdown commands](/private/tmp/wpc-verify-0007-resume-clock/evidence.json), [final macOS log](/private/tmp/wpc-verify-0007-resume-clock/fixed-macos.log), and [final Linux log](/private/tmp/wpc-verify-0007-resume-clock/fixed-linux.log) exercise actual application lanes with `testing/synctest` timers and real SQLite.
6. [Store crash records](/private/tmp/wpc-verify-0007-resume-state/report.json) and [final fault records](/private/tmp/wpc-verify-0007-resume-state/final-report.json) cover actual SIGKILL boundaries, migration rollback, snapshot timing, retained writer ownership, and the repaired fatal diagnostic.
7. [Recovery waits on macOS](/private/tmp/wpc-verify-0007-resume/final-recovery.jsonl) and [Linux](/private/tmp/wpc-verify-0007-resume/final-linux-recovery.jsonl) include empty profiles with retained obligations. [Acquisition budget](/private/tmp/wpc-verify-0007-resume/acquisition-budget.log) and [Linux budget](/private/tmp/wpc-verify-0007-resume/linux-acquisition.log) prove one five minute allowance across phases. [Actual hung socket](/private/tmp/wpc-verify-0007-resume/socket.jsonl) records the wall time experiment using the real Docker command against a private Unix socket that accepts connections without replying.
8. [macOS broken streams](/private/tmp/wpc-verify-0007-resume/final-pipes.json) and [Linux broken streams](/private/tmp/wpc-verify-0007-resume/final-linux-pipes.json) return exit 1. [Repository gate](/private/tmp/wpc-verify-0007-resume/debug-all-gate.log) records formatting, vet, race tests, and lint passing after the fixes. Both arm64 production CLI builds succeeded.

## Verification boundaries

Provider results and remote cleanup responses were controlled. No key was attached to a real provider and no host registration was changed. The actual hung socket experiment contacts only its private local server. These checks prove scheduling and recovery behavior, not current provider entitlement behavior.

The probes use real SQLite with controlled commit delays and lost acknowledgements, real private journal replacements with injected acknowledgement failures, and synthetic monotonic time for long timer cases. The socket timeout and entered SQLite delay were also observed using wall time. Linux verification used local containers with networking disabled.

Three defects found during verification were fixed before this pass: image inspection discarded timeout causes, consecutive scheduled batches reset mutation pacing, and internal cancellation hid fatal SQL errors. Retained regressions cover those failures. The separate `/test scheduled crawl and revalidation` workflow remains available to broaden lasting coverage.
