# Verification plan

This is a plan, not evidence of a passing implementation. Record the tested commit, exact final archive identity, platform, fixture, command or keyboard journey, observed result, and any limit for each scenario.

## Required journeys

| Scenario | Expected proof | Criteria |
| --- | --- | --- |
| Initial read and setup | Start, source options, review, Results, and reports cause no writer admission or external calls; absent state remains absent until reviewed crawl Start | AC-1, AC-14 |
| Crawl through the UI | Select a configured subset, apply a page override, review exact order, start, see committed progress, finish, reopen saved coverage after restart | AC-2, AC-5, AC-6, AC-11 |
| Candidate selection | Focused, marked across page boundaries, and all 338 matching candidates produce exact reviewed membership and complete saved selection | AC-3, AC-5, AC-11 |
| Frozen identity | Change candidate identity or replace collection between review and Start; no new batch or validator action occurs, and replacement collection is untouched | AC-3, AC-12, AC-15 |
| Existing recovery | Earlier interrupted records recover during Start before a later candidate mismatch rejects the new batch; review wording and saved evidence match | AC-3, AC-15 |
| Check engine | Two or more controlled workers overlap within the configured ceiling; saved policy, attempt numbers, retries, waits, pacing and cleanup match explicit command behavior | AC-4, AC-6, AC-8 |
| Busy ownership | Existing runner, separate foreground writer, mismatched service, stale service observation, aliased directory and other profile validator owner each produce correct guidance without external control | AC-5, AC-10 |
| Probe and cooldown | Large selection under required probe fails explicitly; one candidate review uses saved probe policy; missing trusted retry delay stays unavailable | AC-4, AC-6, AC-10 |
| Partial crawl | One source fails and later sources proceed; rate limit skips later work; committed message facts survive and skipped work is not success | AC-2, AC-6, AC-15 |
| Operation and browsing | Navigate and refresh Overview, Results, Details, and reports while checks run; completion changes the indicator without stealing focus | AC-7, AC-12, AC-13 |
| Independent export | Export a fixed view while work commits; cancel each independently, then quit during concurrent publication and cleanup; retain both typed receipts | AC-7 through AC-9, AC-15 |
| Cancellation during preparation | Hold report or export preparation active; Ctrl+X stops only operational work and Ctrl+G stops only preparation, from normal and small screens, help, text entry, and quit confirmation; neither waits for preparation completion | AC-7, AC-8, AC-13 |
| Retained unresolved origin | Finish a check with unresolved cleanup, acknowledge it, run and acknowledge a successful crawl, then quit; the warning and final diagnostic still identify the original check and the exit remains failure | AC-6, AC-7, AC-15 |
| Authoritative recovery | A later explicit check proves all retained obligations resolved under the same owner with durable journal acknowledgement; cleanup warning clears even if later admission is denied. Mere inspection, missing registry, cooldown expiry, and unrelated success do not clear it; independent SQL uncertainty remains latched | AC-4, AC-15 |
| Review races | Double Start, stale view/marks, changed draft, abandoned review preparation, cancel before start acknowledgement, and lost Start reply never duplicate work | AC-3, AC-5, AC-8, AC-15 |
| Cancellation and quit | Immediate cancel, repeated cancel, q Stay, q confirmed quit, Ctrl+C, SIGTERM, hangup, protocol loss and child crash preserve ownership and evidence | AC-8, AC-9, AC-13, AC-15 |
| Hard deadlines | Block remote cleanup, entered SQL, close, reply write, child teardown and final diagnostics; restore the terminal independently and prove first deadline never extends | AC-8, AC-9, AC-15 |
| Boundary races | Signal at operation guard admission, cancel at acknowledged completion, quit after an earlier cancellation, and cancel during report/export cleanup select the correct process deadline | AC-5, AC-8, AC-9, AC-15 |
| Unsafe persistence | Lost acknowledgement during run creation and later commits preserves known versus unknown facts; no later mutation starts in the session; no guessed run ID or zero outcome is rendered | AC-6, AC-15 |
| Complete reports | Modern crawl with multiple source/page boundaries, modern batches with more than 50 items and full attempts, and legacy crawl/check data remain completely reachable | AC-11, AC-12 |
| Report consistency | Concurrent writer changes during preparation cannot mix report times; failed refresh retains the prior report, replacement discards handles, cleanup failure retains the newly activated handle with warning | AC-11, AC-12, AC-15 |
| Display and input | Wide and 60 by 16 layouts, no color, smaller resize, very large exact IDs, text entry, hostile strings, help and scroll preservation keep primary actions usable | AC-13, AC-14 |
| Privacy and wire | Malformed, oversized, duplicate and unknown fields fail safely; all ordinary replies and diagnostics remain key free; late reveal is cleared before work navigation | AC-14, AC-15 |
| Compatibility | Root help/version and nonterminal fallback remain free of state and assets; explicit crawl/check/show/list/export/run/service behavior and protocol bundle mismatch checks pass | AC-1, AC-16 |

## Real integration and release evidence

Run the final matching archives on native Linux amd64 and arm64. Record both normal native terminal behavior and the existing glibc 2.28 baseline package journey. Building an archive or running host tests does not prove terminal or validator behavior on either target.

Use a real public Telegram source to prove reviewed collection, persistence and restart inspection. Use the configured immutable validator image with the current WARP package pin to prove a sourced candidate's attributable entitlement evidence and remote cleanup through the terminal path. Record host registration isolation, shared recovery ownership, cancellation while checking, and deletion reconciliation. If live evidence is unavailable, name the exact missing proof; synthetic providers do not replace it.

After behavior verification, add regression tests for the actual boundaries and run the established formatting, vet, Go race tests, lint, and UI verification gate. Preserve the existing native CI and package checks. Required local commands remain those recorded in root and area `AGENTS.md`; this spec does not introduce a separate test framework or test preference.

## Evidence record

Implementation and verification are pending. No acceptance criterion is marked passed by this document.
