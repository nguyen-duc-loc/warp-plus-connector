# Scheduled crawl and revalidation rationale

## Context

You already have bounded public crawls, complete persisted results, and isolated concurrent checks. Keeping results current still requires manual invocations. A long lived runner must reuse those paths without competing with their writer locks, repeating missed work, or turning uncertain cleanup into permission for another attachment.

This is scope feature 8 on the existing personal macOS and Linux arm64 CLI. The project uses Tracer Bullet delivery and the Beta workflow. The first design choices were independent hourly crawls and six hour routine checks, with foreground execution in this slice and service management in the next one.

## Options considered

### Persisted eligibility with standard library timers

Save a crawl schedule and one eligibility row per candidate, then select a small immutable batch whenever due. This directly supports restart, fair ordering, and explanations through the current database. It costs a schema migration and requires atomic updates alongside existing completion paths.

### Derive every deadline from execution history

Recompute eligibility whenever the process wakes. This avoids a derived scheduling table and makes history the only stored basis. It requires increasingly complex selection queries and still needs durable shared provider restrictions and careful recovery rules.

### Durable jobs or a scheduling framework

A separate job lifecycle or scheduling library could add calendar expressions, explicit jobs, and richer dispatch policies. Those features would help a broader automation product. Here they duplicate saved runs and introduce more lifecycle states without removing the existing validator ownership problem.

### External periodic command invocation

An operating system timer can run existing commands with little application code. It does not provide the chosen foreground loop, per candidate due selection, shared cooldown probes, or simultaneous source collection under one writer without additional coordination. Operating system supervision remains useful for the next slice.

## Rationale

The selected model keeps scheduling proportional to the saved collection rather than elapsed time. Storing the calculation basis makes a due time explainable and lets configuration changes affect the future while preserving evidence. You explicitly chose persisted eligibility despite its derived nature; every change therefore commits with the fact that causes it.

You chose immediate eligibility for new discoveries, routine rechecks for invalid candidates, a separate one hour failure interval, and one overdue execution after downtime. You later refined immediate dispatch to happen between small batches so the current complete selection contract can remain intact. Twice the worker count gives a small fixed working set without adding a new public tuning parameter.

Counting matching manual work avoids redundant checks after you stop the runner to investigate. Coverage equality includes channel order and page limits so a partial manual crawl cannot accidentally postpone broader scheduled coverage. Completion based intervals avoid a backlog when work is slow, at the cost of drifting from fixed wall clock hours.

You chose continued crawling during temporary backend and cleanup failures, automatic cleanup retries, and a durable throttle cooldown followed by one candidate probe. Sharing that gate across profiles requires registry version 3. The worker remains protocol 2 because the change concerns host admission and persistence, not the provider command protocol.

A saved UTC checkpoint deliberately favors explicit suspension over silently moving due times after a backward clock change. This can require correcting the host clock. Monotonic request pacing remains separate from these durable wall time decisions.

## Integration evidence

The repository has 89 Go source files. The relevant existing paths are application crawl and batch coordination, SQLite writer recovery, CLI runtime construction, and the shared Docker registry. Both manual application operations currently open their own writer, so reusable operation cores are required before they can run under one long lived owner. The batch engine saves immutable selection and policy before dispatch, which favors bounded batch boundaries over adding candidates to an active batch.

The current journal stores a version 2 session array and reconstructs that object on every update. The new gate requires preserving additional fields on every write. Existing worker results already distinguish typed throttling and provide a normalized attributable `not_unlimited` response, allowing provider responsiveness to remain separate from entitlement without a protocol change.

Git fetch completed during discovery, but the remote exposed no `origin/main` tracking ref, so a remote behind count was unavailable. The working tree was clean before this spec. No application code, remote registration, container, or service configuration was changed during design.

You selected no separate References section. No new tools or skills were selected, and the earlier discovery decline remains in force.

## Independent review

A read only review by another model found two contradictions and five missing decisions. You approved all seven recommended fixes on 2026-10-11. The revision distinguishes live gate completion from abandoned invocation recovery, persists a recovery obligation across contention, honors saved retry waits at startup, defines interrupted gate reasons, names the inherited five minute acquisition budget, specifies cleanup and finalization deadline arithmetic, and samples inspection time only after establishing its SQLite snapshot. These changes clarify recovery without changing the selected foreground scope or normal eligibility policy.

The targeted followup confirmed that all seven decisions were addressed. It identified one remaining wording inconsistency about a timeout after successful cleanup inspection. That sentence now preserves only an unfulfilled recovery obligation while retaining the backend admission wait. The review's remaining qualification is therefore applied.

You separately accepted the revised spec on 2026-10-11 and authorized linking its build milestones to scope feature 8. The decision is confirmed; the feature remains unbuilt and the spec retains `Proposed` status.
