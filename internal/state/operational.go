package state

import "time"

// OutcomeCounts partitions completed candidate outcomes without exposing keys.
type OutcomeCounts struct {
	Total               int64 `json:"total"`
	Unchecked           int64 `json:"unchecked"`
	Confirmed           int64 `json:"confirmed"`
	Invalid             int64 `json:"invalid"`
	Inconclusive        int64 `json:"inconclusive"`
	DefinitiveConfirmed int64 `json:"definitive_confirmed"`
	DefinitiveInvalid   int64 `json:"definitive_invalid"`
	DefinitiveNone      int64 `json:"definitive_none"`
}

// OperationalSource is the bounded saved source summary for service status.
type OperationalSource struct {
	Channel         string  `json:"channel"`
	Status          string  `json:"status"`
	ReasonCode      *string `json:"reason_code"`
	SavedCandidates int64   `json:"saved_candidates"`
	SavedSightings  int64   `json:"saved_sightings"`
}

// FailureCount counts one terminal reason per selected validation item.
type FailureCount struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

// OperationalRun retains a saved run header and its complete aggregate.
type OperationalRun struct {
	Run                             Run
	SavedCandidates, SavedSightings int64
	Sources                         []OperationalSource
	Totals                          *ValidationTotals
	Failures                        []FailureCount
}

// OperationalWork contains facts from one read snapshot and evaluation time.
type OperationalWork struct {
	AsOf                                     time.Time
	Schedule                                 ScheduleSummary
	Outcomes                                 OutcomeCounts
	LatestCrawl, LastTerminalCrawl           *OperationalRun
	LatestValidation, LastTerminalValidation *OperationalRun
}
