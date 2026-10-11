// Package state defines persisted discoveries and validation evidence independently
// of the CLI, database driver, and validator implementation.
package state

import "time"

// RunKind identifies the work recorded by a run.
type RunKind string

// Supported run kinds.
const (
	Crawl      RunKind = "crawl"
	Validation RunKind = "validation"
)

// RunStatus describes execution, never entitlement.
type RunStatus string

// Supported run states. Only recovery produces Interrupted.
const (
	Running     RunStatus = "running"
	Completed   RunStatus = "completed"
	Failed      RunStatus = "failed"
	Cancelled   RunStatus = "cancelled"
	Interrupted RunStatus = "interrupted"
)

// Outcome describes a completed validation attempt; an empty outcome is unresolved.
type Outcome string

// Unchecked is a summary value and is never stored on an attempt.
const (
	Unchecked    Outcome = "unchecked"
	Confirmed    Outcome = "confirmed"
	Invalid      Outcome = "invalid"
	Inconclusive Outcome = "inconclusive"
)

// EvidenceKind selects the single fact in an evidence record.
type EvidenceKind string

// Supported evidence facts.
const (
	AccountType EvidenceKind = "account_type"
	Rejection   EvidenceKind = "rejection"
)

// Evidence contains normalized facts supplied by a validator.
// The store validates shape, not external entitlement.
type Evidence struct {
	Kind          EvidenceKind
	ObservedAt    time.Time
	AccountType   string
	RejectionCode string
}

// Run retains execution times separately from recovery time.
type Run struct {
	Origin       string
	ID           int64
	RequestID    string
	Kind         RunKind
	Status       RunStatus
	StartedAt    time.Time
	FinishedAt   *time.Time
	RecoveredAt  *time.Time
	ReasonCode   *string
	ClockAnomaly bool
}

// Attempt retains one validation invocation and its optional definitive evidence.
type Attempt struct {
	ID             int64
	RequestID      string
	RunID          int64
	CandidateID    int64
	BackendID      string
	BackendVersion string
	StartedAt      time.Time
	FinishedAt     *time.Time
	RecoveredAt    *time.Time
	Outcome        Outcome
	ReasonCode     *string
	Evidence       *Evidence
	ClockAnomaly   bool
}

// Freshness is evaluated using caller supplied time and maximum age.
type Freshness string

// Supported freshness states.
const (
	NotApplicable Freshness = "not_applicable"
	Fresh         Freshness = "fresh"
	Stale         Freshness = "stale"
	Unknown       Freshness = "unknown"
)

// Evaluation must remain fixed throughout a candidate traversal.
type Evaluation struct {
	AsOf   time.Time
	MaxAge time.Duration
}

// Candidate is a summary without embedded history collections.
type Candidate struct {
	ID                    int64
	Key                   string
	FirstSeenAt           time.Time
	LastSeenAt            time.Time
	LatestOutcome         Outcome
	LatestAttempt         *Attempt
	LastDefinitiveAttempt *Attempt
	UnresolvedAttempt     *Attempt
	Freshness             Freshness
	ClockAnomaly          bool
}

// Sighting is one candidate's provenance in a distinct source message.
type Sighting struct {
	ID          int64
	CandidateID int64
	SourceID    string
	MessageID   string
	MessageURL  *string
	PublishedAt *time.Time
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// Observation links a crawl run to a sighting at its first supplied time.
type Observation struct {
	RunID      int64
	SightingID int64
	ObservedAt time.Time
}

// RunInput supplies a stable retry token and run creation fields.
type RunInput struct {
	Origin    string
	RequestID string
	Kind      RunKind
	StartedAt time.Time
}

// MessageInput is one atomic discovery; message bodies are never accepted.
type MessageInput struct {
	RunID       int64
	SourceID    string
	MessageID   string
	ObservedAt  time.Time
	PublishedAt *time.Time
	MessageURL  *string
	Keys        []string
}

// Discovery identifies a normalized key and its source sighting.
type Discovery struct {
	CandidateID int64
	SightingID  int64
}

// MessageResult lists identities in normalized key byte order and insert counts.
type MessageResult struct {
	Discoveries     []Discovery
	NewCandidates   int
	NewSightings    int
	NewObservations int
}

// AttemptInput identifies a single invocation with a stable retry token.
type AttemptInput struct {
	RequestID      string
	RunID          int64
	CandidateID    int64
	BackendID      string
	BackendVersion string
	StartedAt      time.Time
}

// AttemptCompletion contains the terminal result of one invocation.
type AttemptCompletion struct {
	SchedulePolicy SchedulePolicy
	AttemptID      int64
	FinishedAt     time.Time
	Outcome        Outcome
	ReasonCode     string
	Evidence       *Evidence
}

// RunCompletion supplies the terminal execution result.
type RunCompletion struct {
	SchedulePolicy SchedulePolicy
	RunID          int64
	Status         RunStatus
	FinishedAt     time.Time
	ReasonCode     string
}

// Cursor is portable between processes using the same state database. It carries
// no keys and is not an authorization token. Callers should retain it unchanged.
type Cursor struct {
	Version     int
	Collection  string
	ParentID    int64
	Filter      string
	LastID      int64
	HighWaterID int64
	Evaluation  Evaluation
}

// PageRequest bounds a page, never the total collection. Zero size means 100.
type PageRequest struct {
	Size   int
	Cursor *Cursor
}

// Page contains a bounded collection and an optional continuation.
type Page[T any] struct {
	Items []T
	Next  *Cursor
}

// AttemptFilter selects exactly one candidate or validation run.
type AttemptFilter struct {
	CandidateID int64
	RunID       int64
}
