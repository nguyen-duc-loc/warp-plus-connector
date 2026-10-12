package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
)

// ControlResult describes the acknowledged outcome without implying cleanup success.
type ControlResult struct {
	Action         string
	Changed        bool
	InstallationID string
	State          string
}

func publishDefinition(m Manifest) error {
	data, err := renderDefinition(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath), 0700); err != nil {
		return failure("service_access_error")
	}
	f, err := privateOpen(m.DefinitionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		// An identical publication from an interrupted install may be adopted.
		if e := verifyDefinition(m); e != nil {
			return failure("service_conflict")
		}
		return privatefs.SyncDirectory(filepath.Dir(m.DefinitionPath))
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Sync(), f.Close())
	if err != nil {
		return failure("service_access_error")
	}
	return privatefs.SyncDirectory(filepath.Dir(m.DefinitionPath))
}

// Install enables login startup only after validating all installation inputs.
func Install(ctx context.Context, configPath string) (result ControlResult, err error) {
	result.Action = "install"
	a, err := ResolveAccount(ctx)
	if err != nil {
		return result, err
	}
	wanted, err := prepare(ctx, a, configPath)
	if err != nil {
		return result, err
	}
	if err := preflight(ctx, a, wanted); err != nil {
		return result, err
	}
	if _, e := a.manifest(); missing(e) {
		if _, e := os.Lstat(wanted.DefinitionPath); !missing(e) {
			return result, failure("service_conflict")
		}
		s, e := inspect(ctx, a, nil)
		if e != nil {
			return result, e
		}
		if s.Loaded {
			return result, failure("service_conflict")
		}
	} else if e != nil {
		return result, e
	}
	if err := directory(a.Root); err != nil {
		return result, err
	}
	lock, err := controlLock(filepath.Join(a.Root, "control.lock"))
	if err != nil {
		return result, err
	}
	defer func() { err = uncertain(errors.Join(err, lock.Close()), result.Changed) }()
	m, err := a.manifest()
	if err != nil && !missing(err) {
		return result, err
	}
	if err == nil {
		result.InstallationID = m.InstallationID
		wanted.InstallationID, wanted.InstalledAt = m.InstallationID, m.InstalledAt
		wanted.Operation, wanted.Phase, wanted.AutostartEnabled = m.Operation, m.Phase, m.AutostartEnabled
		data, e := renderDefinition(wanted)
		if e != nil {
			return result, e
		}
		wanted.DefinitionSHA256 = digest(data)
		if !reflect.DeepEqual(wanted, m) {
			return result, failure("service_conflict")
		}
		if m.Phase == "ready" {
			if _, err := checkedNative(ctx, a, m); err != nil {
				return result, err
			}
			result.State = "installed"
			return result, nil
		}
		if m.Operation != "install" {
			return result, failure("service_operation_incomplete")
		}
	} else {
		if _, e := os.Lstat(wanted.DefinitionPath); !missing(e) {
			return result, failure("service_conflict")
		}
		s, e := inspect(ctx, a, nil)
		if e != nil {
			return result, e
		}
		if s.Loaded {
			return result, failure("service_conflict")
		}
		m = wanted
		result.InstallationID, result.Changed = m.InstallationID, true
		if err := m.save(); err != nil {
			return result, err
		}
	}
	result.Changed = true
	if m.Phase == "prepared" {
		if err := publishDefinition(m); err != nil {
			return result, err
		}
		m.Phase = "definition_written"
		if err := m.save(); err != nil {
			return result, err
		}
	}
	if m.Phase == "definition_written" {
		if err := verifyDefinition(m); err != nil {
			return result, err
		}
		if err := reload(ctx, a); err != nil {
			return result, err
		}
		if _, err := checkedNative(ctx, a, m); err != nil {
			return result, err
		}
		if err := startup(ctx, a, m, true); err != nil {
			return result, err
		}
		m.Phase = "manager_applied"
		enabled := true
		m.AutostartEnabled = &enabled
		if err := m.save(); err != nil {
			return result, err
		}
	}
	m.Phase = "ready"
	if err := m.save(); err != nil {
		return result, err
	}
	result.State = "installed"
	return result, nil
}

// Control performs a repeatable mutation under one account control lock.
func Control(ctx context.Context, action string) (result ControlResult, err error) {
	result.Action = action
	if !oneOf(action, "start", "stop", "restart", "enable", "disable", "uninstall") {
		return result, failure("invalid_arguments")
	}
	a, err := ResolveAccount(ctx)
	if err != nil {
		return result, err
	}
	if err := checkDirectory(a.Root); err != nil {
		if missing(err) && action == "uninstall" {
			return absentUninstall(ctx, a)
		}
		if missing(err) {
			return result, failure("service_not_installed")
		}
		return result, err
	}
	lock, err := controlLock(filepath.Join(a.Root, "control.lock"))
	if err != nil {
		return result, err
	}
	defer func() { err = uncertain(errors.Join(err, lock.Close()), result.Changed) }()
	m, err := a.manifest()
	if missing(err) && action == "uninstall" {
		return absentUninstall(ctx, a)
	}
	if missing(err) {
		return result, failure("service_not_installed")
	}
	if err != nil {
		return result, err
	}
	result.InstallationID = m.InstallationID
	if m.Phase != "ready" && action != m.Operation {
		return result, failure("service_operation_incomplete")
	}
	switch action {
	case "enable", "disable":
		if _, err := checkedNative(ctx, a, m); err != nil {
			return result, err
		}
		enabled := action == "enable"
		if m.Phase == "ready" && m.AutostartEnabled != nil && *m.AutostartEnabled == enabled {
			// Reapply acknowledged intent to correct an external startup override.
			if err := startup(ctx, a, m, enabled); err != nil {
				result.Changed = true
				return result, err
			}
			result.State = action + "d"
			return result, nil
		}
		result.Changed = true
		if m.Phase == "ready" {
			m.Operation, m.Phase = action, "prepared"
			if err := m.save(); err != nil {
				return result, err
			}
		}
		if m.Phase == "prepared" {
			if err := startup(ctx, a, m, enabled); err != nil {
				return result, err
			}
			m.AutostartEnabled, m.Phase = &enabled, "manager_applied"
			if err := m.save(); err != nil {
				return result, err
			}
		}
		m.Phase = "ready"
		if err := m.save(); err != nil {
			return result, err
		}
		result.State = action + "d"
	case "start":
		return start(ctx, a, m, result)
	case "stop":
		return stop(ctx, a, m, result)
	case "restart":
		result, err = stop(ctx, a, m, result)
		if err != nil {
			return result, err
		}
		return start(ctx, a, m, result)
	case "uninstall":
		return uninstall(ctx, a, m, result)
	}
	return result, nil
}

func start(ctx context.Context, a Account, m Manifest, result ControlResult) (ControlResult, error) {
	if err := executable(m.ExecutablePath); err != nil {
		return result, err
	}
	if m.AutostartEnabled == nil || !*m.AutostartEnabled {
		return result, failure("service_disabled")
	}
	s, err := checkedNative(ctx, a, m)
	if err != nil {
		return result, err
	}
	baseline, err := readObservation(a.Root)
	if err != nil && !missing(err) {
		return result, err
	}
	owner, err := ownerPID(filepath.Join(a.Root, "runner.lock"))
	if err != nil {
		return result, err
	}
	if owner != nil && *owner > 0 {
		if s.Identity != "verified" || s.PID == nil || *owner != *s.PID || baseline.PID != *owner ||
			baseline.InstallationID != m.InstallationID || baseline.terminal() {
			return result, failure("service_identity_unverified")
		}
		result.State = "starting"
		if baseline.Phase == "running" || baseline.Phase == "clock_paused" {
			result.State = "running"
		}
		return result, nil
	}
	if s.Running != nil && *s.Running {
		return result, failure("service_identity_unverified")
	}
	result.Changed = true
	if a.Manager == "launchd" {
		if !s.Loaded {
			_, err = native(ctx, a, 10*time.Second, "bootstrap", "gui/"+strconv.Itoa(a.UID), m.DefinitionPath)
		} else {
			_, err = native(ctx, a, 10*time.Second, "kickstart", target(a))
		}
	} else {
		_, err = native(ctx, a, 10*time.Second, "start", a.Name)
	}
	if err != nil {
		return result, uncertain(err, true)
	}
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		first, e := readObservation(a.Root)
		if e == nil && first.InstallationID == m.InstallationID && first.InvocationID != baseline.InvocationID {
			s, e := inspect(wait, a, &m)
			pid, pe := ownerPID(filepath.Join(a.Root, "runner.lock"))
			second, se := readObservation(a.Root)
			if e == nil && pe == nil && se == nil && first.InvocationID == second.InvocationID &&
				first.PID == second.PID && second.InstallationID == m.InstallationID {
				noProcess := !s.Loaded || (s.Running != nil && !*s.Running &&
					(s.PendingJob == nil || !*s.PendingJob))
				if first.Phase == "failed" && second.Phase == "failed" && pid != nil && *pid == 0 && noProcess {
					return result, failure("service_start_failed")
				}
				if pid != nil && s.PID != nil && *pid == first.PID && *s.PID == first.PID &&
					oneOf(first.Phase, "running", "clock_paused") && oneOf(
					second.Phase,
					"running",
					"clock_paused",
				) {
					result.State = "running"
					return result, nil
				}
			}
		}
		select {
		case <-wait.Done():
			if ctx.Err() != nil {
				return result, uncertain(failure("cancelled"), true)
			}
			result.State = "starting"
			return result, nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func stop(parent context.Context, a Account, m Manifest, result ControlResult) (ControlResult, error) {
	ctx, cancel := context.WithTimeout(parent, 80*time.Second)
	defer cancel()
	s, err := checkedNative(ctx, a, m)
	if err != nil {
		return result, err
	}
	pid, err := ownerPID(filepath.Join(a.Root, "runner.lock"))
	if err != nil {
		return result, err
	}
	if nativeGone(s) && (pid == nil || *pid == 0) {
		result.State = "stopped"
		return result, nil
	}
	if pid != nil && *pid > 0 && (s.PID == nil || *s.PID != *pid) {
		return result, failure("service_identity_unverified")
	}
	result.Changed = true
	if a.Manager == "launchd" {
		_, err = native(ctx, a, 80*time.Second, "bootout", target(a))
	} else {
		_, err = native(ctx, a, 80*time.Second, "stop", a.Name)
	}
	if err != nil {
		return result, uncertain(failure("service_stop_timeout"), true)
	}
	for {
		s, e := inspect(ctx, a, &m)
		pid, pe := ownerPID(filepath.Join(a.Root, "runner.lock"))
		if e == nil && pe == nil && nativeGone(s) && (pid == nil || *pid == 0) {
			result.State = "stopped"
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, uncertain(failure("service_stop_timeout"), true)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// RemovalReceipt retains the last acknowledged native and definition removal.
type RemovalReceipt struct {
	SchemaVersion       int       `json:"schema_version"`
	InstallationID      string    `json:"installation_id"`
	UID                 int       `json:"uid"`
	Manager             string    `json:"manager"`
	ServiceName         string    `json:"service_name"`
	DefinitionPath      string    `json:"definition_path"`
	DefinitionSHA256    string    `json:"definition_sha256"`
	DefinitionRemovedAt time.Time `json:"definition_removed_at"`
}

func uninstall(ctx context.Context, a Account, m Manifest, result ControlResult) (ControlResult, error) {
	lock, err := runnerLock(filepath.Join(a.Root, "runner.lock"))
	if err != nil {
		return result, err
	}
	defer func() { _ = lock.Close() }()
	m, err = a.manifest()
	if err != nil {
		return result, err
	}
	if m.Phase == "ready" || m.Phase == "prepared" {
		s, err := checkedStoppedNative(ctx, a, m)
		if err != nil {
			return result, err
		}
		if !nativeGone(s) {
			return result, failure("service_running")
		}
		result.Changed = true
		m.Operation, m.Phase = "uninstall", "prepared"
		if err := m.save(); err != nil {
			return result, err
		}
		if err := startup(ctx, a, m, false); err != nil {
			return result, err
		}
		// launchd's inactive registered jobs lack identity proof and were rejected above.
		s, err = checkedStoppedNative(ctx, a, m)
		if err != nil || !nativeGone(s) {
			return result, failure("service_identity_unverified")
		}
		b := false
		m.AutostartEnabled, m.Phase = &b, "manager_applied"
		if err := m.save(); err != nil {
			return result, err
		}
	}
	result.Changed = true
	if m.Phase == "manager_applied" {
		if _, err := os.Lstat(m.DefinitionPath); err == nil {
			if _, err := checkedStoppedNative(ctx, a, m); err != nil {
				return result, err
			}
		} else if !missing(err) {
			return result, failure("service_access_error")
		}
		if err := verifyDefinition(m); err != nil && !missing(err) {
			return result, err
		}
		if err := privatefs.Remove(m.DefinitionPath); err != nil {
			return result, err
		}
		if err := privatefs.SyncDirectory(filepath.Dir(m.DefinitionPath)); err != nil {
			return result, err
		}
		m.Phase = "definition_removed"
		if err := m.save(); err != nil {
			return result, err
		}
	}
	if _, err := os.Lstat(m.DefinitionPath); !missing(err) {
		return result, failure("service_conflict")
	}
	if a.Manager == "systemd" {
		s, err := linuxState(ctx, a, &m)
		if err != nil {
			return result, err
		}
		if s.Identity == "unverified" || !nativeGone(s) {
			return result, failure("service_conflict")
		}
	}
	if err := reload(ctx, a); err != nil {
		return result, err
	}
	s, err := inspect(ctx, a, nil)
	if err != nil {
		return result, err
	}
	if s.Loaded {
		return result, failure("service_conflict")
	}
	if a.Manager == "systemd" {
		m.Phase = "manager_removed"
		if err := m.save(); err != nil {
			return result, err
		}
	}
	receipt := RemovalReceipt{
		SchemaVersion: 1, InstallationID: m.InstallationID, UID: m.UID,
		Manager: m.Manager, ServiceName: m.ServiceName, DefinitionPath: m.DefinitionPath,
		DefinitionSHA256: m.DefinitionSHA256, DefinitionRemovedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := writeRecord(filepath.Join(a.Root, "removal.json"), receipt); err != nil {
		return result, err
	}
	if err := privatefs.Remove(filepath.Join(a.Root, "installation.json")); err != nil {
		return result, err
	}
	result.State = "uninstalled"
	return result, nil
}

// This query never opens runner.lock because uninstall already owns that inode.
func checkedStoppedNative(ctx context.Context, a Account, m Manifest) (ManagerState, error) {
	if err := verifyDefinition(m); err != nil {
		return ManagerState{}, err
	}
	var s ManagerState
	var err error
	if a.Manager == "launchd" {
		s, err = macList(ctx, a)
	} else {
		s, err = linuxState(ctx, a, &m)
	}
	if err != nil {
		return s, err
	}
	if s.Identity == "unverified" {
		return s, failure(nativeIdentityCode(a.Manager))
	}
	if !nativeGone(s) {
		return s, failure("service_running")
	}
	return s, nil
}

func absentUninstall(ctx context.Context, a Account) (ControlResult, error) {
	r := ControlResult{Action: "uninstall", InstallationID: "none", State: "uninstalled"}
	var receipt RemovalReceipt
	err := readRecord(filepath.Join(a.Root, "removal.json"), &receipt)
	if err != nil && !missing(err) {
		return r, err
	}
	path := definitionPath(a, filepath.Join(a.Home, ".config"))
	if err == nil {
		if receipt.SchemaVersion != 1 || receipt.UID != a.UID || receipt.Manager != a.Manager ||
			receipt.ServiceName != a.Name || !identityPattern.MatchString(receipt.InstallationID) ||
			!safePath(receipt.DefinitionPath) || len(receipt.DefinitionSHA256) != 64 ||
			!validTime(receipt.DefinitionRemovedAt) {
			return r, failure("service_record_invalid")
		}
		path, r.InstallationID = receipt.DefinitionPath, receipt.InstallationID
	}
	if _, err := os.Lstat(path); !missing(err) {
		return r, failure("service_conflict")
	}
	s, err := inspect(ctx, a, nil)
	if err != nil {
		return r, err
	}
	if s.Loaded {
		return r, failure("service_conflict")
	}
	if err := checkDirectory(a.Root); err == nil {
		if err := privatefs.SyncDirectory(a.Root); err != nil {
			return r, failure("service_access_error")
		}
	}
	return r, nil
}
