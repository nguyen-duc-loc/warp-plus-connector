package state

import "time"

// OverviewCollection contains complete aggregates from one saved read snapshot.
type OverviewCollection struct {
	AsOf                                             time.Time
	MaxAge                                           time.Duration
	Outcomes                                         OutcomeCounts
	FreshConfirmed, StaleConfirmed, UnknownFreshness int64
	ClockAnomaly                                     bool
	RecentRuns                                       []OperationalRun
	OlderRunsAvailable                               bool
	Schedule                                         ScheduleSummary
}

// OverviewComponent retains independent observation and cache facts.
type OverviewComponent[T any] struct {
	Availability string
	ObservedAt   time.Time
	ClockAnomaly bool
	CachePolicy  string
	ErrorCode    *string
	Data         *T
}

// OverviewInstallation exposes only installation facts needed by the overview.
type OverviewInstallation struct {
	InstallationID, Phase, StateDir string
	AutostartEnabled                *bool
}

// OverviewManager describes independently observed native manager facts.
type OverviewManager struct {
	Loaded                        bool
	Running                       *bool
	PID, LastExitCode, LastSignal *int
	ActiveState                   *string
	PendingJob, AutostartObserved *bool
}

// OverviewObservation is saved runner evidence without private invocation details.
type OverviewObservation struct {
	Phase, CrawlPhase, ValidationPhase string
	StartedAt, UpdatedAt               time.Time
	StoppedAt                          *time.Time
	FailureCode                        *string
}

// OverviewRunner keeps ownership separate from retained observations.
type OverviewRunner struct {
	LockHeld                   *bool
	OwnerPID                   *int
	InstallationMatches, Stale *bool
	ClockAnomaly               bool
	Observation                *OverviewObservation
}

// OverviewService carries identity even when all components are unavailable.
type OverviewService struct {
	IdentityState, IdentityCachePolicy string
	InstallationID                     *string
	CollectionMatch                    string
	BoundStateDir                      *string
	Installation                       OverviewComponent[OverviewInstallation]
	Manager                            OverviewComponent[OverviewManager]
	Runner                             OverviewComponent[OverviewRunner]
}

// Overview is one collection snapshot and independently observed service evidence.
type Overview struct {
	HeaderAt   time.Time
	Collection OverviewComponent[OverviewCollection]
	Service    OverviewService
}
