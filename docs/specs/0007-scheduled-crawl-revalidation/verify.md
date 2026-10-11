# Scheduled crawl and revalidation verification

This is an acceptance plan, not evidence of implementation. Use controlled source and validator adapters with the real store and application coordinator for timing and fault cases. Exercise the production command with real parsing, output streams, and subprocess signals. Inject clocks and timers through internal test boundaries; do not add a public fast interval flag or weaken production interval limits.

| Scenario | Observable result | Criteria |
| --- | --- | --- |
| Fresh state, one source, one candidate | One scheduled crawl and check survive restart; inspection explains the next due time | AC-1, AC-3, AC-4, AC-11, AC-12, AC-13 |
| Every invalid flag and configuration boundary | Failure precedes state creation or external work; disabled lane flags preserve explicit false | AC-1 |
| Validation disabled, then crawling disabled | Each independent lane operates; both disabled fails; saved eligibility survives reenabling | AC-1, AC-7 |
| Slow check, retry wait, and next crawl deadline | Crawl requests complete while validation remains active; no second crawl or batch overlaps its own lane | AC-2 |
| Worker and mutation observations | At most configured workers execute; command spacing and one candidate attempt invariants hold | AC-2, AC-10 |
| 338 due candidates and repeated bounded selections | Every candidate is reached in deterministic order with no collection truncation; each selection is within its fixed bound | AC-3, AC-15 |
| New discoveries during a held batch | They become due immediately and enter selection after that immutable batch ends | AC-3 |
| Confirmation, invalidity, exhausted retries, rediscovery | Routine and failure intervals use terminal facts; evidence freshness does not move on rediscovery or inconclusive results | AC-4 |
| Downtime spanning many intervals | One due crawl and bounded candidate selection occur, with no replayed missed occurrences | AC-5 |
| Manual exact, subset, changed, and reordered coverage | Only the exact saved full plan advances that schedule; changed unsatisfied coverage is due immediately | AC-6 |
| Interval changes and repeated restarts | Due times recompute from unchanged bases; restart itself does not perpetually postpone work | AC-5, AC-7 |
| Backward UTC while idle, active, and across restart | New external admission pauses, cleanup remains possible, deadlines and evidence remain intact | AC-7, AC-14 |
| Forward UTC and resume after clock repair | Overdue work is coalesced and one probe at most is admitted | AC-7, AC-9 |
| Backend unavailable, then available | Crawls continue, retries are paced, no fake candidate attempts precede successful acquisition | AC-8 |
| Empty profile starts during validator contention or a shared clock wait | Saved recovery obligation survives restart and triggers inspection after the wait, without requiring a candidate or creating a probe | AC-5, AC-8, AC-10 |
| Repeated restart before a saved backend or cleanup deadline | No early acquisition occurs and the failure basis does not move; a configured interval change recomputes from that same basis | AC-5, AC-7, AC-8 |
| Docker accepts a connection but never responds, with no candidate | One acquisition and recovery attempt uses a single five minute budget across phases, retains its obligation, then waits the failure interval | AC-8, AC-14 |
| Cleanup uncertain, then proven | No registration occurs before proof; retry waits survive restart and crawling continues | AC-8 |
| Permanent identity, permissions, image, or journal fault | Explicit fatal diagnostic and bounded stop, with no automatic mutation loop | AC-8, AC-10, AC-14 |
| Profile A throttles, profile B attempts work | Both manual and scheduled checks respect the original shared deadline; a shorter interval cannot bypass it | AC-9 |
| Cooldown expires with several due candidates | One saved probe uses one worker and attempt; normal batches resume only after healthy response, cleanup, and commit | AC-9 |
| Probe fails, is cancelled, or crashes | New cooldown or retained recovery state prevents immediate repeated admission | AC-9, AC-14 |
| Manual multiple ID check during required probe | Complete ID validation remains intact; command returns probe required without a partial run | AC-9 |
| `validator recover` during cooldown | Cleanup can finish but the provider restriction remains | AC-8, AC-9 |
| Two runners, manual writer, and concurrent reader | Losing writers perform no external work; listing, export, and schedule inspection remain usable | AC-10 |
| Inspection while another process commits | All counts and references in one response use one snapshot; keys and raw provider text are absent | AC-11 |
| Writer commits a checkpoint immediately around inspection startup | Snapshot establishment precedes the fixed clock sample; a normal commit cannot cause a false clock regression warning | AC-7, AC-11 |
| Missing, empty, old, and damaged state | No read creates or repairs state; output has the defined empty or error semantics | AC-11, AC-13 |
| Stdout or stderr failure and SIGINT or SIGTERM | No repeated writes to a failed stream; cleanup and committed history survive with the defined exit status | AC-12, AC-14 |
| Remote cleanup ends at 40 seconds or consumes the 60 second allowance | Shared local finalization deadlines are respectively stop plus 45 or 65 seconds, with no new writes after the applicable deadline | AC-14 |
| Schema migration and recovery fault injection | Version, eligibility, references, and existing history remain atomic | AC-13 |
| Registry versions 1, 2, 3, and unknown newer version | Safe conversion requires proven cleanup; older executables reject version 3; unsupported versions fail closed | AC-9, AC-13 |
| Crash between journal admission, result commit, and gate clearing | Extra waiting is possible; extra unguarded admission is not | AC-9, AC-13, AC-14 |
| Profile B recovers profile A while A's database is unavailable | Proven cleanup creates a cooldown with the explicit interrupted or probe failure reason; B never finalizes A's run, and opening still requires a new healthy committed probe | AC-5, AC-9, AC-13 |

Run the repository formatting, vet, race, and lint gate and both arm64 builds after implementation. Record platform runtime results separately from compilation. For live validation, request authorization for the actual keys and experiment before attaching them. Record remote cleanup evidence, shared gate state, and unchanged host registration without putting secrets in this repository. A controlled fixture is not evidence of current real provider behavior.

The Beta sequence is behavior verification first, then the separate test workflow for meaningful retained regressions. Fill this file with actual evidence and any explicit limitations after the feature exists.
