# Persistent state

You can use `internal/state` for input and result records and `internal/store` for persistence. The CLI does not open this store yet.

The store uses `modernc.org/sqlite v1.60.1` through `database/sql`. It supplies SQLite without a C toolchain on macOS and Linux arm64. Explicit SQL and one embedded migration keep schema changes inside this package. Connection options follow the [driver documentation](https://pkg.go.dev/modernc.org/sqlite@v1.60.1).

## Opening and writing

You supply an absolute, account private state directory and a clock to `OpenWriter`. The writer owns an advisory lock until `Close`. Opening recovers unfinished work, so closing a writer with active runs does not imply that those runs completed.

A typical crawl uses `StartRun`, `RecordMessage`, then `FinishRun`. You supply a stable `crypto/rand.Text()` request token when starting a run or attempt. Retrying a store call uses that same token. A new validation invocation uses a new token.

One message write is atomic. Candidate text is trimmed, then compared exactly. Its result includes identities in key byte order and counts of records inserted by that call.

Validation evidence is typed. Only controlled fixtures supply definitive results in this slice. Persisting a result does not prove external entitlement or pass the separate feasibility gate.

## Reading

Both handles provide all read methods. `OpenReader` opens existing state without migration or recovery. Each handle has its own read connection. Writer reads use a separate connection from mutations.

`GetCandidate` and `ListCandidates` require an explicit evaluation time and positive maximum age. Summaries expose the latest completed attempt, last definitive attempt, and any unresolved attempt separately.

Every list uses `state.PageRequest`. A zero size selects 100 records per page. Sizes from 1 through 1000 are allowed. You can follow `Next` until it is nil. Cursors survive JSON serialization and contain no keys. You keep the parent filter and evaluation fixed. Membership is bounded at the first page, while details can change between pages. Observation traversal requires a terminal crawl run.

## Cancellation and errors

Errors support `errors.Is`. Driver error text and its unwrap chain never leave the package. Record contents are available only through authorized read results, not diagnostic text.

SQLite driver v1.60.1 can keep waiting inside its native busy handler after context cancellation. Connections use a five second busy timeout, reduced for an earlier operation deadline. Calls return cancellation promptly while entered work retains its resources until rollback and cleanup. No transaction is retried automatically. `Close` waits for entered work before releasing writer ownership. After an uncertain write error, you can retry the same operation identity.

## Development checks

`go test ./internal/store -v` exercises the public interface, with narrow internal fixtures for SQL faults, migrations, and synchronized process crashes. The same suite has run on macOS arm64 and in a Linux arm64 container. The repository gate runs formatting, vet, race tests, and lint.

The Beta workflow still calls for the separate `/check verify persistent key state` and `/test persistent key state` passes.
