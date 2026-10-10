package state

import "time"

// CrawlInput creates a crawl and its immutable requested coverage atomically.
type CrawlInput struct {
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
	SavedCandidates int64
	SavedSightings  int64
}
