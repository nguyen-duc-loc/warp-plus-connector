package service

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// Component names the availability, time, and source of one independent observation.
type Component struct {
	Availability string    `json:"availability"`
	ObservedAt   time.Time `json:"observed_at"`
	ErrorCode    *string   `json:"error_code"`
	Data         any       `json:"data"`
}

// StatusReport deliberately avoids merging manager and runner evidence into health.
type StatusReport struct {
	SchemaVersion int       `json:"schema_version"`
	View          string    `json:"view"`
	ObservedAt    time.Time `json:"observed_at"`
	Complete      bool      `json:"complete"`
	Installation  Component `json:"installation"`
	Manager       Component `json:"manager"`
	Runner        Component `json:"runner"`
	Work          Component `json:"work"`
}

// RunnerStatus exposes owner evidence independently of the retained observation.
type RunnerStatus struct {
	LockHeld            *bool        `json:"lock_held"`
	OwnerPID            *int         `json:"owner_pid"`
	InstallationMatches *bool        `json:"installation_matches"`
	Stale               *bool        `json:"stale"`
	ClockAnomaly        bool         `json:"clock_anomaly"`
	Observation         *Observation `json:"observation"`
}

func installationStatus(m Manifest) map[string]any {
	return map[string]any{
		"installation_id": m.InstallationID, "manager": m.Manager, "service_name": m.ServiceName,
		"phase": m.Phase, "operation": m.Operation, "autostart_enabled": m.AutostartEnabled,
		"executable_path": m.ExecutablePath, "config_path": m.ConfigPath, "state_dir": m.StateDir,
		"validator_state_dir": m.ValidatorStateDir, "docker_executable": m.DockerExecutable,
		"docker_socket": m.DockerSocket,
	}
}

func timestamp() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
func component(data any, err error) Component {
	c := Component{Availability: "available", ObservedAt: timestamp(), Data: data}
	if err != nil {
		c.Data = nil
		c.Availability = "unavailable"
		if missing(err) || errors.Is(err, store.ErrNotInitialized) {
			c.Availability = "missing"
		}
		code := diagnosticCode(err)
		c.ErrorCode = &code
	}
	return c
}

func diagnosticCode(err error) string {
	for _, pair := range []struct {
		err  error
		code string
	}{
		{store.ErrNotInitialized, "state_not_initialized"}, {store.ErrSchemaTooNew, "state_schema_too_new"},
		{store.ErrMigrationRequired, "state_migration_required"}, {store.ErrStateAccess, "state_error"},
		{context.DeadlineExceeded, "service_diagnostic_timeout"}, {context.Canceled, "cancelled"},
	} {
		if errors.Is(err, pair.err) {
			return pair.code
		}
	}
	if missing(err) {
		return "service_not_installed"
	}
	return stableCode(err)
}

func runnerStatus(a Account, m *Manifest) Component {
	var out RunnerStatus
	o, observationErr := readObservation(a.Root)
	observed := timestamp()
	if observationErr == nil {
		out.Observation = &o
		out.ClockAnomaly = o.UpdatedAt.After(observed) || o.UpdatedAt.Before(o.StartedAt) ||
			(o.StoppedAt != nil && (o.StoppedAt.Before(o.StartedAt) || o.StoppedAt.After(o.UpdatedAt)))
		if !out.ClockAnomaly {
			stale := observed.Sub(o.UpdatedAt) > 60*time.Second
			out.Stale = &stale
		}
		if m != nil {
			matches := m.InstallationID == o.InstallationID
			out.InstallationMatches = &matches
		}
	} else if !missing(observationErr) {
		return component(nil, observationErr)
	}
	pid, err := ownerPID(filepath.Join(a.Root, "runner.lock"))
	if err != nil {
		return component(nil, failure("service_owner_unknown"))
	}
	if pid != nil {
		held := *pid > 0
		out.LockHeld = &held
		if held {
			out.OwnerPID = pid
		}
	}
	c := component(out, nil)
	c.ObservedAt = observed
	return c
}

func readWork(ctx context.Context, path string) (work state.OperationalWork, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := store.OpenReader(ctx, path)
	if err != nil {
		return work, err
	}
	work, err = r.GetOperationalWork(ctx, time.Now)
	err = errors.Join(err, r.Close())
	return work, err
}

// Status inspects independent components and rechecks installation identity last.
func Status(ctx context.Context, a Account) StatusReport {
	out := StatusReport{SchemaVersion: 1, View: "service_status", Complete: true}
	m, err := a.manifest()
	out.Installation = component(nil, err)
	var bound *Manifest
	if err == nil {
		bound = &m
		out.Installation.Data = installationStatus(m)
	}
	s, managerErr := inspect(ctx, a, bound)
	out.Manager = component(s, managerErr)
	out.Runner = runnerStatus(a, bound)
	if bound == nil {
		out.Work = component(nil, failure("service_not_installed"))
	} else {
		w, e := readWork(ctx, m.StateDir)
		out.Work = component(w, e)
	}
	latest, latestErr := a.manifest()
	changed := (err == nil) != (latestErr == nil) || (err == nil && latest.InstallationID != m.InstallationID)
	if err != nil && latestErr != nil && missing(err) != missing(latestErr) {
		changed = true
	}
	if changed {
		for _, c := range []*Component{&out.Installation, &out.Manager, &out.Runner, &out.Work} {
			*c = component(nil, failure("service_changed"))
		}
	}
	for _, c := range []Component{out.Installation, out.Manager, out.Runner, out.Work} {
		if c.Availability == "unavailable" {
			out.Complete = false
		}
	}
	out.ObservedAt = timestamp()
	return out
}
