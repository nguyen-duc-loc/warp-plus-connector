package state

import (
	"errors"
	"time"
)

// SchedulePolicy retains exact durations independently of evidence freshness.
type SchedulePolicy struct{ Crawl, Routine, Failure time.Duration }

// DefaultSchedulePolicy is the compatibility policy for embedded store callers.
func DefaultSchedulePolicy() SchedulePolicy {
	return SchedulePolicy{Crawl: time.Hour, Routine: 6 * time.Hour, Failure: time.Hour}
}

// Valid requires positive durations; the runner additionally enforces its bounds.
func (p SchedulePolicy) Valid() bool { return p.Crawl > 0 && p.Routine > 0 && p.Failure > 0 }

// ScheduleDue rounds upward so submicrosecond intervals never become early.
func ScheduleDue(at time.Time, interval time.Duration) (time.Time, error) {
	if at.IsZero() || interval < 0 {
		return time.Time{}, errors.New("invalid schedule time")
	}
	due := at.UTC().Add(interval)
	if due.Nanosecond()%1000 != 0 {
		due = due.Truncate(time.Microsecond).Add(time.Microsecond)
	}
	if due.Year() < 1 || due.Year() > 9999 {
		return time.Time{}, errors.New("invalid schedule time")
	}
	return due, nil
}

// SchedulePlan explains saved crawl eligibility.
type SchedulePlan struct {
	Coverage                []SourcePlan
	DueAt                   time.Time
	Reason                  string
	BasisAt                 time.Time
	Interval                time.Duration
	BasisRunID, ActiveRunID *int64
}

// CandidateSchedule explains one candidate without including its key.
type CandidateSchedule struct {
	CandidateID                             int64
	DueAt                                   time.Time
	Reason                                  string
	BasisAt                                 time.Time
	Interval                                time.Duration
	BasisRunID, BasisAttemptID, ActiveRunID *int64
	EligibleByTime                          bool
}

// ValidationWait is a local observation, not a claim of live provider readiness.
type ValidationWait struct {
	Reason                       string
	BasisAt, RetryAt, ObservedAt time.Time
	Interval                     time.Duration
}

// ScheduleCounts partition the entire collection within one snapshot.
type ScheduleCounts struct {
	Total, Active, Due, Waiting int64
	NextDueAt                   *time.Time
}

// ScheduleSummary contains saved facts evaluated after establishing one snapshot.
type ScheduleSummary struct {
	AsOf, CheckpointAt                        time.Time
	ClockRegressed, ValidationRecoveryPending bool
	Crawl                                     *SchedulePlan
	Candidates                                ScheduleCounts
	ValidationWait                            *ValidationWait
	Candidate                                 *CandidateSchedule
}
