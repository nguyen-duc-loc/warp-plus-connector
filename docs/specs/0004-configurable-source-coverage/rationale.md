# Configurable source coverage rationale

## Context

Scope feature 5 expands a completed Go CLI that currently fetches one `warpplus` preview page. The application, source adapter, domain coverage types, and SQLite coverage checks all encode that fixed source and page count. Merely adding a channel flag would not provide durable reports for several sources or pages.

You chose four enabled seed channels, a page budget with per channel overrides, and one invocation report. Earlier message commits must survive a later failure, while public source visibility stays separate from WARP entitlement. The project uses Beta verification and Tracer Bullet delivery on macOS and Linux arm64.

At design start, the working tree contained completed Slice 1 work that was not committed. That work is now recorded in commit `64211b4`. A repository fetch completed, but no remote branch references were available to measure how far the local branch was behind. This spec describes the inspected workspace. No live Telegram pagination research was requested or performed during this design.

## Options considered

### Option 1: Expand existing coverage in place

Generalize the existing table and treat each channel as a separate run. This reuses much of the current store interface and avoids a source child lifecycle. It makes one invocation harder to inspect and still requires a new representation for page history. You chose one run for the whole invocation.

### Option 2: Extend packages and add source and page records

Keep the existing discovery and run identities, add child coverage records, and preserve the legacy report path. This gives each invocation one durable plan and keeps earlier history intact without fabricating details. It adds lifecycle checks, a page observation link, and support for two historical report formats. This is the chosen option.

### Option 3: Replace crawl storage and source management

Introduce a database source registry and replace the original crawl representation with one uniform model. This could support source management commands and resumed crawling later. It would migrate working history and move editable settings out of the chosen private JSON model before either capability is needed.

## Rationale

Adding records beside legacy coverage limits the migration to new schema objects. One source row per selected channel captures both intended coverage and sources never reached after cancellation or rate limiting. A page observation link gives exact committed counts without storing message bodies or every post ID. Separate parsed facts and saved counts explain a write failure without claiming all extracted candidates were saved.

JSON replacement semantics make the source list match what you wrote. A merge with hidden defaults is a viable alternative, but makes removing seeds less obvious. Lowercase source identities prevent spelling variants from splitting provenance. Exact candidate case remains unchanged because channel identity and key identity have different rules.

Sequential fetching, finite page budgets, and one second pacing keep the request policy explicit without introducing a worker pool. Parallel source fetching is the runner up if elapsed time becomes a measured problem. Stopping the whole invocation on 429 treats the shared Telegram endpoint as rate limited and avoids automatic retries. Other source failures remain isolated.

Navigation is accepted only from a recognized control and converted into a cursor for an internally constructed URL. Following arbitrary links is simpler but would expand the network boundary. Guessing older cursors from parsed post IDs avoids a navigation parser but could invent traversal that the page never advertised. The chosen design records observed coverage without promising complete history.

Implementation details settled in this record are the new table names, lifecycle rules, stable reason codes, page observation link, integer limits, cursor validation, and source report derivations. They implement your confirmed model and operating choices. No new library, service, environment configuration layer, or community tooling is needed.

An independent model reviewed the draft. You approved its recommended clarifications on 2026-10-11. Mutation errors now stop further writes conservatively because the current store contract cannot always prove whether a commit occurred. The application attests page completion after successful message writes, avoiding a new message manifest. The review also made child reason codes, request timestamps, and report count sources explicit. The feature status remains `Proposed` under the project's lifecycle rule, including after design acceptance.
