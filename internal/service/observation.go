package service

import (
	"path/filepath"
	"time"
)

// Policy records the settings actually observed by a managed invocation.
type Policy struct {
	CrawlInterval   string `json:"crawl_interval"`
	RecheckInterval string `json:"recheck_interval"`
	FailureInterval string `json:"failure_interval"`
	Workers         int    `json:"workers"`
}

// Observation is retained evidence of a runner phase, never proof of liveness.
type Observation struct {
	SchemaVersion     int        `json:"schema_version"`
	InstallationID    string     `json:"installation_id"`
	InvocationID      string     `json:"invocation_id"`
	PID               int        `json:"pid"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	Phase             string     `json:"phase"`
	CrawlEnabled      *bool      `json:"crawl_enabled"`
	ValidationEnabled *bool      `json:"validation_enabled"`
	CrawlPhase        string     `json:"crawl_phase"`
	ValidationPhase   string     `json:"validation_phase"`
	Policy            *Policy    `json:"policy"`
	StoppedAt         *time.Time `json:"stopped_at"`
	FailureCode       *string    `json:"failure_code"`
	AppExitCode       *int       `json:"app_exit_code"`
}

func (o Observation) terminal() bool { return o.Phase == "stopped" || o.Phase == "failed" }

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

func validTime(t time.Time) bool {
	_, offset := t.Zone()
	return !t.IsZero() && offset == 0 && t.Nanosecond()%1000 == 0
}

func readObservation(root string) (Observation, error) {
	var o Observation
	if err := checkDirectory(root); err != nil {
		return o, err
	}
	if err := readRecord(filepath.Join(root, "observation.json"), &o); err != nil {
		return o, err
	}
	if o.SchemaVersion != 1 || !identityPattern.MatchString(o.InstallationID) ||
		!identityPattern.MatchString(o.InvocationID) || o.PID <= 0 || !validTime(o.StartedAt) || !validTime(o.UpdatedAt) {
		return o, failure("service_record_invalid")
	}
	if !oneOf(o.Phase, "starting", "running", "clock_paused", "stopping", "stopped", "failed") ||
		!oneOf(o.CrawlPhase, "unknown", "disabled", "idle", "crawling", "clock_paused", "stopping") ||
		!oneOf(
			o.ValidationPhase,
			"unknown",
			"disabled",
			"idle",
			"acquiring",
			"checking",
			"waiting",
			"clock_paused",
			"stopping",
		) {
		return o, failure("service_record_invalid")
	}
	if o.terminal() != (o.StoppedAt != nil) || (o.StoppedAt != nil && !validTime(*o.StoppedAt)) ||
		(o.Phase == "failed") != (o.FailureCode != nil) ||
		(o.FailureCode != nil && (*o.FailureCode == "" || !validCode(*o.FailureCode))) ||
		(o.terminal() != (o.AppExitCode != nil)) {
		return o, failure("service_record_invalid")
	}
	if p := o.Policy; p != nil {
		if o.CrawlEnabled == nil || o.ValidationEnabled == nil || p.Workers < 1 || p.Workers > 4 {
			return o, failure("service_record_invalid")
		}
		for _, value := range []string{p.CrawlInterval, p.RecheckInterval, p.FailureInterval} {
			d, err := time.ParseDuration(value)
			if err != nil || d < time.Minute || d > 720*time.Hour || d.String() != value {
				return o, failure("service_record_invalid")
			}
		}
	} else if o.CrawlEnabled != nil || o.ValidationEnabled != nil {
		return o, failure("service_record_invalid")
	}
	return o, nil
}
