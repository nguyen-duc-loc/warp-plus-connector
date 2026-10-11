package state

import "time"

// CheckPolicy is the immutable versioned policy, with durations in microseconds.
type CheckPolicy struct {
	Version            int   `json:"version"`
	Workers            int   `json:"workers"`
	MaxAttempts        int   `json:"max_attempts"`
	MutationIntervalUS int64 `json:"mutation_interval_us"`
	CandidateTimeoutUS int64 `json:"candidate_timeout_us"`
	AttemptTimeoutUS   int64 `json:"attempt_timeout_us"`
	CommandTimeoutUS   int64 `json:"command_timeout_us"`
	PollTimeoutUS      int64 `json:"poll_timeout_us"`
	PollIntervalUS     int64 `json:"poll_interval_us"`
	CleanupTimeoutUS   int64 `json:"cleanup_timeout_us"`
	ShutdownTimeoutUS  int64 `json:"shutdown_timeout_us"`
	FinalizeTimeoutUS  int64 `json:"finalize_timeout_us"`
	RetryBaseUS        int64 `json:"retry_base_us"`
	RetryCapUS         int64 `json:"retry_cap_us"`
	MemoryMiB          int   `json:"memory_mib"`
	CPUs               int   `json:"cpus"`
	Processes          int   `json:"processes"`
	ProtocolVersion    int   `json:"protocol_version"`
}

// DefaultCheckPolicy supplies the complete policy version 1 snapshot.
func DefaultCheckPolicy() CheckPolicy {
	return CheckPolicy{
		1, 2, 3, 1000000, 900000000, 240000000, 60000000, 10000000, 250000,
		60000000, 60000000, 5000000, 5000000, 60000000, 512, 1, 256, 2,
	}
}

// Valid rejects unsupported versions, bounds, and changes to fixed policy values.
func (p CheckPolicy) Valid() bool {
	if p.Workers < 1 || p.Workers > 4 || p.MaxAttempts < 1 || p.MaxAttempts > 5 ||
		p.MutationIntervalUS < 1000000 || p.MutationIntervalUS > 60000000 || p.MutationIntervalUS%1000000 != 0 ||
		p.CandidateTimeoutUS < 60000000 || p.CandidateTimeoutUS > 3600000000 {
		return false
	}
	d := DefaultCheckPolicy()
	d.Workers = p.Workers
	d.MaxAttempts = p.MaxAttempts
	d.MutationIntervalUS = p.MutationIntervalUS
	d.CandidateTimeoutUS = p.CandidateTimeoutUS
	return d == p
}

// FailureClass is a backend supplied scheduling fact, never parsed diagnostic text.
type FailureClass string

// Supported failure classifications.
const (
	Temporary FailureClass = "temporary"
	Throttled FailureClass = "throttled"
	Terminal  FailureClass = "terminal"
	Unsafe    FailureClass = "unsafe"
)

// ValidationBatchInput saves the complete ordered selection before external work.
type ValidationBatchInput struct {
	RequestID                 string
	StartedAt                 time.Time
	CandidateIDs              []int64
	BackendID, BackendVersion string
	Policy                    CheckPolicy
}

// ValidationItem retains execution independently of entitlement.
type ValidationItem struct {
	RunID, CandidateID                 int64
	Ordinal                            int
	Status, StopReason                 string
	StartedAt, FinishedAt, RecoveredAt *time.Time
	Attempts                           int
	LatestAttempt                      *Attempt
}

// BatchAttempt retains scheduling facts alongside a real attempt.
type BatchAttempt struct {
	Attempt         Attempt
	Number          int
	FailureClass    FailureClass
	RetryWait       time.Duration
	RetryEligibleAt *time.Time
}

// BatchAttemptInput supplies intent for one sequential attempt.
type BatchAttemptInput struct {
	AttemptInput
	Number int
}

// BatchAttemptCompletion atomically resolves an attempt and its next disposition.
type BatchAttemptCompletion struct {
	AttemptCompletion
	FailureClass    FailureClass
	RetryWait       time.Duration
	RetryEligibleAt *time.Time
	StopReason      string
}

// ValidationItemCompletion closes an item without creating an attempt.
type ValidationItemCompletion struct {
	RunID, CandidateID int64
	FinishedAt         time.Time
	StopReason         string
}

// ValidationTotals are derived from saved items and their latest attempts.
type ValidationTotals struct {
	Selected, Pending, Active, Completed, Skipped, Interrupted int
	Confirmed, Invalid, Inconclusive, Unresolved, Unstarted    int
}

// ValidationReport is a header snapshot; nil policy denotes legacy history.
type ValidationReport struct {
	Run                       Run
	BackendID, BackendVersion string
	Policy                    *CheckPolicy
	Totals                    ValidationTotals
}

// CheckRecord streams a saved header, item, or attempt through bounded reads.
type CheckRecord struct {
	Header  *ValidationReport
	Item    *ValidationItem
	Attempt *BatchAttempt
}
