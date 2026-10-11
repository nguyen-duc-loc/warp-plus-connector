# Shared validator cooldown

## Summary

A provider throttle pauses new validation across every profile using the shared recovery owner. Once the wait ends, one candidate tests whether normal work can resume. Cleanup remains available throughout, and a crash cannot silently erase the restriction.

## Registry contract

Advance the host registry to version 3 under the existing shared private owner lock. Session manifests, cleanup receipts, container commands, and the pinned image retain worker protocol 2. The registry retains an empty version 3 document after sessions are removed so an older executable rejects it.

The registry has required `version`, `sessions`, and `last_observed_at`, plus nullable `gate`. The maximum four sessions and all existing identity checks remain unchanged. `last_observed_at` is the maximum UTC microsecond timestamp observed under this owner. The optional gate fields are:

| Field | Type and presence | Meaning |
| --- | --- | --- |
| `phase` | Required enum `active`, `cooldown`, or `probe` | Current durable admission restriction |
| `basis_at` | Required UTC timestamp | Time this phase or cooldown was established |
| `retry_interval_ns` | Required positive integer | Effective failure interval of the invocation establishing the gate |
| `not_before` | Required timestamp for `cooldown`, absent otherwise | Earliest time a probe may be admitted |
| `reason` | Required normalized code | `validation_active`, `validation_rate_limited`, `validation_probe`, `validation_probe_failed`, or `validation_interrupted` |

An absent gate means open. `active` records that a normal batch owns permission and may leave unfinished remote work. `probe` records exclusive permission for one candidate. Durable phase intent is written before the first registration or attachment in that owned batch. If no remote mutation was admitted, ownership may be released without installing a phase. Active owner state is runtime knowledge backed by the held lock; after a new process acquires the lock, any retained `active` or `probe` phase is an interrupted invocation.

The interval is captured when a gate is created. Another profile with a shorter interval cannot shorten the saved deadline. Changing state directory, image, or local Docker endpoint does not clear it. It is intentionally scoped to the existing recovery owner, not a candidate or database. No database record is required to interpret or enforce it.

### Migration

Read and validate the prior registry while holding the owner lock. For versions 1 and 2, use their existing recovery rules and prove retained cleanup before conversion. If cleanup is still unknown, retain the older version and its truthful partial recovery progress, then return cleanup pending. With clean prior state, atomically write version 3 with an empty session list, a supplied UTC checkpoint, and no gate. Do not rebuild the worker image or infer a cooldown from old diagnostic text.

SQLite migration and registry conversion are separate. A crash after either one may leave the other pending; the next writer or validator acquisition can finish its own migration. Neither migration clears the other store's restrictions. Unknown later versions and malformed fields fail closed. A failed replacement or directory synchronization poisons journal writes for that invocation, including when rename already succeeded.

## Admission and completion

Every manual or scheduled Docker check resolves its complete candidate selection before saving a run. Under the shared owner lock, validate the registry, reconcile retained sessions, and read gate state. Cleanup is allowed even when admission is denied. Cooldown inspection and clock checks need no new registration.

An open gate permits a normal batch. After the batch is durably saved and before its first remote mutation, save `active` with the invocation's failure interval. The existing coordinator still limits workers and serializes registration and attachment. On ordinary terminal completion, keep the phase until SQLite finalization succeeds and all remote sessions have proven cleanup and removal. Only then save an open gate and release ownership. A failed SQL acknowledgement does not permit clearing `active`.

For the pinned Docker adapter, a typed throttle without a trusted delay immediately stops new mutations and establishes `cooldown` using `observed_at + failure_interval`, rounded upward to a UTC microsecond. Write the cooldown before allowing later admission or clearing session records. Cancellation and cleanup of already active sessions follow the existing batch stop rules. A later response cannot shorten an existing cooldown; retain the greatest deadline. If a gate capable adapter ever supplies a positive trusted server delay, that delay is also a lower bound. The selected Docker version supplies none; this feature does not enable another backend.

The generic batch coordinator retains the existing positive server delay retry contract for adapters without this shared Docker gate. The Docker gate is an additional constraint, and its terminal stop and subsequent probe govern its next invocation.

Before `not_before`, checks return `validator_cooldown` with a typed retry timestamp before creating a new run or attempt. The CLI may render that safe timestamp on stderr. A scheduler saves the observation and sleeps without retrying remote mutations. A manual caller returns an error immediately.

Once the deadline passes, automatic selection is limited to one candidate. A manual `check` must also select exactly one distinct candidate; otherwise return `validator_probe_required` without a run or partial selection. Save the probe run with effective workers and maximum attempts both one. Install `probe` before remote admission, carrying forward the originating cooldown's saved interval. Keep the shared lock through its complete cleanup and durable result so two profiles cannot both probe.

### Probe result

A healthy probe needs all of these facts: an attributable provider response, no throttle or transport failure, proven cleanup, container removal, and acknowledged local terminal persistence. The adapter exposes a typed `ProviderResponsive` fact separately from entitlement. For protocol 2 it may derive that fact only from validated attributable confirmation, validated definitive rejection, or the normalized `not_unlimited` response that follows a matching registration and license read. An arbitrary terminal error, capacity error text, command success, unknown response, or mere absence of throttling is insufficient. This host fact does not change the container protocol or turn an inconclusive entitlement result into a definitive result.

A healthy probe clears the gate after its SQLite result commits. The next batch may use normal capacity. An unhealthy probe, cancellation, or incomplete cleanup preserves or establishes a cooldown from the final failure observation plus its saved interval, with reason `validation_probe_failed`; an actual throttle retains `validation_rate_limited`. Repeated probes use that interval, without an additional increasing delay policy. Unknown cleanup still independently forbids registration, even after the cooldown expires.

`validator recover` can reconcile and remove sessions during a cooldown, but it never counts as a provider probe and never opens the gate. There is no force, bypass, or reset flag.

## Crash and clock handling

After acquiring a retained `active` or `probe` phase from an earlier invocation, first reconcile its sessions. Then replace the phase with a cooldown based on the current supplied recovery time and the saved interval. This conservative wait also covers a lost acknowledgement after healthy completion. If cleanup cannot be proven, retain the phase and sessions and return cleanup pending; a later successful recovery starts the cooldown once. An already saved cooldown keeps its deadline across restart and repeated recovery. Do not move it forward merely because another reader or runner observed it.

This abandoned invocation path is the explicit exception to live completion's SQLite ordering. Recovery needs the released owner lock, the journal, and remote cleanup proof; it neither opens nor finalizes another profile's database. It cannot open the gate directly. The resulting cooldown must expire and a fresh healthy probe must commit its own result before normal admission resumes. The abandoned run is independently marked interrupted when its own database writer next opens.

| Transition | Saved phase and reason |
| --- | --- |
| Open gate admits a normal live batch | `active`, `validation_active` |
| Expired cooldown admits one live probe | `probe`, `validation_probe` |
| Typed throttle in a live invocation or retained recovery result | `cooldown`, `validation_rate_limited` |
| Abandoned normal `active` phase, no validated throttle | `cooldown`, `validation_interrupted` |
| Abandoned `probe` phase, or an unhealthy live probe without throttle | `cooldown`, `validation_probe_failed` |
| Successful live normal completion or healthy live probe, with required cleanup and acknowledged SQLite result | Gate absent, with no reason |

When multiple restrictions apply, retain the greatest deadline. For equal or differing deadlines, a validated throttle takes reason precedence over probe failure, which takes precedence over interrupted normal work. The chosen reason explains the restriction, while the saved deadline retains the strongest timing bound. An already persisted cooldown observed without new evidence keeps its complete saved fields.

During recovery, consume any validated retained worker result before forgetting its session. A retained throttled result establishes or extends the cooldown. If a result cannot be classified safely, the interrupted active or probe rule applies. An interrupted invocation never supplies a healthy probe result for automatic opening.

Compare admission time against the registry's checkpoint. If UTC is behind it, return `validator_clock_regressed` and the checkpoint as the retry time. Preserve existing deadlines. Cleanup may continue, and its journal writes retain the maximum checkpoint rather than lowering it. When time catches up, normal gate evaluation resumes. Forward jumps may make a probe eligible but never admit multiple probes.

## Boundary changes

Extend the validator owner boundary with typed gate inspection, admission intent, throttle observation, and terminal gate resolution. Supply clock values and the effective failure interval explicitly; do not read application configuration in the adapter. The application coordinator serializes these journal operations with its existing mutation executor. Store access stays in the application and store packages, not the adapter.

Owner acquisition returns typed distinctions for contention, cooldown, clock regression, recoverable unavailability, pending cleanup, and fatal state or identity errors, together with `CleanupInspected` when inspection and cleanup proof actually completed. These are control facts, not parsed diagnostic strings. Acquisition and recovery share the existing five minute attempt budget, bounded by the parent context; shutdown cleanup is further constrained by the scheduler child's shared stop deadlines. Every path, including `validator recover`, preserves the gate when rewriting the session array. Session updates must copy the complete registry metadata; the current helper that reconstructs a version 2 registry from sessions alone cannot remain unchanged.

The gate protects admission, not delivery of an already issued provider request. A request can have reached the provider before a crash. Durable session intent, conservative phase recovery, and proven cleanup bound that uncertainty; no exactly once remote execution claim is made.

## Rationale

A shared journal gate uses the ownership boundary that already prevents conflicting Docker sessions across profiles. A profile only SQLite deadline would be simpler but another profile could bypass it. Requiring manual resumption is the runner up; it reduces automation risk but would leave this personal unattended workflow stalled after an ordinary throttle.
