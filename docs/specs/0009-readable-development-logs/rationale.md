# Readable development logs rationale

## Context

Scope feature 11 calls for activity that is easy to scan during development. Existing foreground logs use full slog text records and mostly info severity, including individual validator mutations. Service text expands each saved field into separate lines. Both formats expose useful evidence, but neither gives a compact view of the work.

The repository has 134 Go source files at design time. Relevant logging is concentrated in the application, CLI, and managed service boundary. The service archive deliberately stores a limited set of events without candidate identities. Presentation improvements therefore need to respect both rich foreground activity and narrower historical evidence.

The project targets Linux amd64 and arm64 and uses the Beta workflow. Results, diagnostics, saved state, cleanup ownership, and private keys already have separate contracts. Formatting should not add reads, alter acknowledgments, or make terminal capability determine whether cleanup runs.

## Options considered

| Option | Benefit | Cost |
| --- | --- | --- |
| Improve slog in place | Fewest application changes and no new logger dependency | Requires a custom readable handler and does not meet your selected zerolog direction |
| Use zerolog behind slog | Reuses the existing application interface and service capture while adding console formatting | Retains two logger abstractions and does not fulfill your choice to replace slog throughout |
| Replace slog with zerolog | One selected logger API for all application activity, with shared console presentation | Requires migration of managed capture and a compatibility writer for existing structured text |

## Rationale

You selected zerolog and explicitly chose a full replacement after considering its slog adapter. The migration remains bounded because logging call sites are concentrated and stored data does not change. The build first proves one real crawl through the new path, then expands to concurrent work and managed capture. A separate long lived migration flag would add another supported logging path without protecting a data migration.

You chose major events at normal verbosity, automatic terminal presentation, local time with date and offset context, visible but subdued identifiers, command flags only, and existing structured text for redirected foreground diagnostics. These choices favor everyday readability while keeping diagnostic correlation and explicit machine formats available.

You also chose consistent final error presentation and service filtering after the existing latest N selection. Keeping JSON unfiltered preserves its schema and record counts. Keeping archive writes independent of display options prevents a developer preference from erasing diagnostic evidence.

Synchronous serialized writes retain output failure control and exact ordering. A background queue would introduce loss, flush, and shutdown decisions without a demonstrated throughput need. The zerolog writer failure latch is necessary because a formatting library's fallback stderr behavior cannot own application cleanup or safely render arbitrary errors.

The chosen dependency version is an implementation target based on the API inspected during design. Compatibility and a tested version must be established by the build. No external logging account, infrastructure, or new skill discovery is needed. You requested no References section.

## Independent review

An independent model found two contract gaps: severity categories did not name the actual cancellation statuses and failure codes, and the foreground field descriptions did not distinguish string source identities from integer record IDs. It also recommended deriving historical recovery restrictions only from each entry's saved code and invoking cancellation callbacks after releasing the output lock.

You authorized all four fixes. The build contract now names the exact severity rules, event fields, types, duration representation, and supplying values. Validator session IDs are omitted because attempt IDs already correlate foreground work. The verification scenarios cover those distinctions, absent historical reasons, and a single callback after unlocking. This review does not establish implementation or runtime verification.
