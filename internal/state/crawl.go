package state

import "time"

// CrawlInput creates a crawl and its immutable requested coverage atomically.
type CrawlInput struct {
	Origin       string
	RequestID    string
	StartedAt    time.Time
	SourceID     string
	RequestedURL string
	CoverageMode string
	PageLimit    int
}

// CrawlPage supplies the facts observed from a fully parsed page.
type CrawlPage struct {
	RunID           int64
	FetchedAt       time.Time
	ParsedPostCount int
	MinMessageID    *int64
	MaxMessageID    *int64
}

// CrawlCoverage distinguishes requested coverage from optional parsed facts.
type CrawlCoverage struct {
	RunID           int64
	SourceID        string
	RequestedURL    string
	CoverageMode    string
	PageLimit       int
	FetchedAt       *time.Time
	ParsedPostCount *int
	MinMessageID    *int64
	MaxMessageID    *int64
}

// CrawlReport reads coverage and committed counts in one transaction.
// Counts describe current progress while the run is running.
type CrawlReport struct {
	Run             Run
	Coverage        *CrawlCoverage
	Sources         []CrawlSource
	SavedCandidates int64
	SavedSightings  int64
}

// SourcePlan is an immutable normalized channel and its bounded coverage.
type SourcePlan struct {
	Channel   string `json:"channel"`
	PageLimit int    `json:"page_limit"`
}

// NormalizeChannel validates bare ASCII names and normalizes their identity.
func NormalizeChannel(channel string) (string, bool) {
	if len(channel) < 1 || len(channel) > 64 {
		return "", false
	}
	b := []byte(channel)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
			continue
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return "", false
	}
	return string(b), true
}

// CrawlPlanInput creates a run and all selected sources atomically.
type CrawlPlanInput struct {
	Origin    string
	RequestID string
	StartedAt time.Time
	Sources   []SourcePlan
}

// CrawlPageID identifies a page within a saved source plan.
type CrawlPageID struct {
	RunID      int64
	SourceID   string
	PageNumber int
}

// CrawlRequest persists intent before fetching its derived URL.
type CrawlRequest struct {
	CrawlPageID
	RequestedAt time.Time
}

// CrawlParse contains fully validated page facts.
type CrawlParse struct {
	CrawlPageID
	FetchedAt                                time.Time
	ParsedPostCount                          int
	MinMessageID, MaxMessageID, NextBeforeID *int64
}

// CrawlPageCompletion attests all message writes for a completed page succeeded.
type CrawlPageCompletion struct {
	CrawlPageID
	Status     string
	FinishedAt time.Time
	ReasonCode string
}

// CrawlSourceCompletion records a source's bounded outcome.
type CrawlSourceCompletion struct {
	RunID      int64
	SourceID   string
	Status     string
	FinishedAt time.Time
	StopReason string
}

// CrawlSource is a saved source snapshot with ordered page records and counts.
type CrawlSource struct {
	RunID                              int64
	SourceID, Channel                  string
	Position, PageLimit                int
	Status                             string
	StartedAt, FinishedAt, RecoveredAt *time.Time
	StopReason                         *string
	Pages                              []CrawlPageRecord
	SavedCandidates, SavedSightings    int64
}

// CrawlPageRecord distinguishes durable request intent from optional parsed facts.
type CrawlPageRecord struct {
	CrawlPageID
	RequestedURL, Status                     string
	RequestedAt                              time.Time
	BeforeID                                 *int64
	FetchedAt, FinishedAt, RecoveredAt       *time.Time
	ParsedPostCount                          *int
	MinMessageID, MaxMessageID, NextBeforeID *int64
	ReasonCode                               *string
	SavedCandidates, SavedSightings          int64
}
