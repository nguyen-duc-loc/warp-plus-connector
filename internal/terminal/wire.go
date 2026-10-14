package terminal

import (
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"strconv"
	"time"
)

type record = map[string]any

func decimal(v int64) string { return strconv.FormatInt(v, 10) }
func optionalInt(v *int) any {
	if v == nil {
		return nil
	}
	return strconv.Itoa(*v)
}
func optionalID(v *int64) any {
	if v == nil {
		return nil
	}
	return decimal(*v)
}
func stamp(t time.Time) any {
	local := t.In(time.Local)
	_, offset := local.Zone()
	return record{
		"utc":   t.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"local": local.Format("2006-01-02 15:04:05"), "utc_offset_seconds": strconv.Itoa(offset),
	}
}
func optionalStamp(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return stamp(*t)
}

func envelope[T any](c state.OverviewComponent[T], at time.Time, encode func(T) any) any {
	var data, age any
	anomaly := c.ClockAnomaly || c.ObservedAt.After(at)
	if !anomaly {
		age = decimal(int64(at.Sub(c.ObservedAt)))
	}
	if c.Data != nil {
		data = encode(*c.Data)
	}
	return record{
		"availability": c.Availability, "observed_at": stamp(c.ObservedAt), "age_ns": age,
		"clock_anomaly": anomaly, "cache_policy": c.CachePolicy, "error_code": c.ErrorCode, "data": data,
	}
}

func overviewResult(o state.Overview) any {
	s := o.Service
	return record{
		"header_at": stamp(o.HeaderAt), "collection": envelope(o.Collection, o.HeaderAt, collectionRecord),
		"service": record{
			"identity": record{
				"state": s.IdentityState, "installation_id": s.InstallationID,
				"cache_policy": s.IdentityCachePolicy,
			}, "collection_match": s.CollectionMatch, "bound_state_dir": s.BoundStateDir,
			"installation": envelope(
				s.Installation, o.HeaderAt, func(i state.OverviewInstallation) any {
					return record{
						"installation_id": i.InstallationID, "phase": i.Phase,
						"autostart_enabled": i.AutostartEnabled, "state_dir": i.StateDir,
					}
				},
			),
			"manager": envelope(
				s.Manager, o.HeaderAt, func(m state.OverviewManager) any {
					return record{
						"loaded": m.Loaded, "running": m.Running, "pid": optionalInt(m.PID),
						"last_exit_code": optionalInt(m.LastExitCode), "last_signal": optionalInt(m.LastSignal),
						"active_state": m.ActiveState, "pending_job": m.PendingJob,
						"autostart_observed": m.AutostartObserved,
					}
				},
			),
			"runner": envelope(s.Runner, o.HeaderAt, runnerRecord),
		},
	}
}

func runnerRecord(r state.OverviewRunner) any {
	var observation any
	if o := r.Observation; o != nil {
		observation = record{
			"phase": o.Phase, "started_at": stamp(o.StartedAt), "updated_at": stamp(o.UpdatedAt),
			"stopped_at": optionalStamp(o.StoppedAt), "crawl_phase": o.CrawlPhase,
			"validation_phase": o.ValidationPhase, "failure_code": o.FailureCode,
		}
	}
	return record{
		"lock_held": r.LockHeld, "owner_pid": optionalInt(r.OwnerPID),
		"installation_matches": r.InstallationMatches, "stale": r.Stale,
		"clock_anomaly": r.ClockAnomaly, "observation": observation,
	}
}

func collectionRecord(c state.OverviewCollection) any {
	runs := make([]any, 0, len(c.RecentRuns))
	for _, run := range c.RecentRuns {
		runs = append(runs, runRecord(run))
	}
	return record{
		"as_of": stamp(c.AsOf), "max_age_ns": decimal(int64(c.MaxAge)), "total": decimal(c.Outcomes.Total),
		"latest_outcomes": record{
			"unchecked": decimal(c.Outcomes.Unchecked), "confirmed": decimal(c.Outcomes.Confirmed),
			"invalid": decimal(c.Outcomes.Invalid), "inconclusive": decimal(c.Outcomes.Inconclusive),
		},
		"fresh_confirmed": decimal(c.FreshConfirmed), "stale_confirmed": decimal(c.StaleConfirmed),
		"unknown_freshness": decimal(c.UnknownFreshness), "recent_runs": runs,
		"older_runs_available": c.OlderRunsAvailable,
		"schedule":             scheduleRecord(c.Schedule),
	}
}

func runRecord(r state.OperationalRun) any {
	run := r.Run
	origin := run.Origin
	if origin == "" {
		origin = "unknown"
	}
	var crawl, validation any
	if run.Kind == state.Crawl {
		crawl = record{"saved_candidates": decimal(r.SavedCandidates), "saved_sightings": decimal(r.SavedSightings)}
	}
	if v := r.Totals; v != nil {
		validation = record{
			"selected": strconv.Itoa(v.Selected), "pending": strconv.Itoa(v.Pending),
			"active": strconv.Itoa(v.Active), "completed": strconv.Itoa(v.Completed),
			"skipped":     strconv.Itoa(v.Skipped),
			"interrupted": strconv.Itoa(v.Interrupted), "confirmed": strconv.Itoa(v.Confirmed),
			"invalid":      strconv.Itoa(v.Invalid),
			"inconclusive": strconv.Itoa(v.Inconclusive), "unresolved": strconv.Itoa(v.Unresolved),
			"unstarted": strconv.Itoa(v.Unstarted),
		}
	}
	return record{
		"id": decimal(run.ID), "kind": run.Kind, "origin": origin, "status": run.Status,
		"started_at": stamp(run.StartedAt), "finished_at": optionalStamp(run.FinishedAt),
		"recovered_at": optionalStamp(run.RecoveredAt), "reason_code": run.ReasonCode,
		"clock_anomaly": run.ClockAnomaly, "crawl_counts": crawl, "validation_counts": validation,
	}
}

func scheduleRecord(s state.ScheduleSummary) any {
	var crawl, wait any
	if c := s.Crawl; c != nil {
		coverage := make([]any, 0, len(c.Coverage))
		for _, p := range c.Coverage {
			coverage = append(coverage, record{"channel": p.Channel, "page_limit": strconv.Itoa(p.PageLimit)})
		}
		crawl = record{
			"coverage": coverage, "due_at": stamp(c.DueAt), "reason": c.Reason, "basis_at": stamp(c.BasisAt),
			"interval_ns": decimal(int64(c.Interval)), "basis_run_id": optionalID(c.BasisRunID),
			"active_run_id": optionalID(c.ActiveRunID),
		}
	}
	if w := s.ValidationWait; w != nil {
		wait = record{
			"reason": w.Reason, "basis_at": stamp(w.BasisAt), "retry_at": stamp(w.RetryAt),
			"observed_at": stamp(w.ObservedAt), "interval_ns": decimal(int64(w.Interval)),
		}
	}
	return record{
		"checkpoint_at": optionalStamp(&s.CheckpointAt), "clock_regressed": s.ClockRegressed,
		"validation_recovery_pending": s.ValidationRecoveryPending, "crawl": crawl,
		"candidates": record{
			"total": decimal(s.Candidates.Total), "active": decimal(s.Candidates.Active),
			"due": decimal(s.Candidates.Due), "waiting": decimal(s.Candidates.Waiting),
			"next_due_at": optionalStamp(s.Candidates.NextDueAt),
		},
		"validation_wait": wait,
	}
}
