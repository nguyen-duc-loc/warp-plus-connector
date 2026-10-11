package validator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
)

type registry struct {
	Version  int        `json:"version"`
	Sessions []manifest `json:"sessions"`
}

type dockerOwner struct {
	docker           *Docker
	lock             *os.File
	endpoint, daemon string
	registry         registry
	poisoned         bool
}

// Acquire reconciles every retained session while holding shared ownership.
func (d *Docker) Acquire(ctx context.Context) (BatchOwner, error) { return d.acquire(ctx, true) }

func (d *Docker) acquire(parent context.Context, checkImage bool) (owner *dockerOwner, err error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	lock, err := d.lock()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = lock.Close()
		}
	}()
	endpoint, err := d.endpoint(ctx)
	if err != nil {
		return nil, err
	}
	daemon, err := d.runtime(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	owner = &dockerOwner{
		docker: d, lock: lock, endpoint: endpoint, daemon: daemon,
		registry: registry{Version: 2, Sessions: []manifest{}},
	}
	var raw map[string]json.RawMessage
	err = privatefs.Read(d.path(), &raw)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, &Error{Code: "validator_state_error"}
	}
	if err == nil {
		var version int
		if json.Unmarshal(raw["version"], &version) != nil {
			return nil, &Error{Code: "validator_state_error"}
		}
		switch version {
		case 1:
			if err = d.reconcile(ctx, endpoint, daemon); err != nil {
				return nil, err
			}
		case 2:
			if err = privatefs.Read(d.path(), &owner.registry); err != nil {
				return nil, &Error{Code: "validator_state_error"}
			}
			if err = owner.validate(); err != nil {
				return nil, err
			}
		default:
			return nil, &Error{Code: "validator_version_mismatch"}
		}
	}
	if err = owner.reconcile(ctx); err != nil {
		return nil, err
	}
	// Keep an empty registry so older executables reject the new contract.
	if err = owner.save(owner.registry.Sessions); err != nil {
		return nil, err
	}
	if checkImage {
		if err = d.image(ctx, endpoint); err != nil {
			return nil, err
		}
	}
	return owner, nil
}

func (o *dockerOwner) Close() error { return o.lock.Close() }

func (o *dockerOwner) validate() error {
	if o.registry.Version != 2 || o.registry.Sessions == nil || len(o.registry.Sessions) > 4 {
		return &Error{Code: "validator_state_error"}
	}
	seen := map[string]bool{}
	for _, m := range o.registry.Sessions {
		if m.Version != 2 || !tokenPattern.MatchString(m.SessionID) || seen[m.SessionID] ||
			m.Name != "wpc-validator-"+strings.ToLower(m.SessionID) || !ValidImage(m.Image) ||
			(m.ContainerID != "" && !idPattern.MatchString(m.ContainerID)) || m.CreatedAt.IsZero() ||
			!filepath.IsAbs(m.Profile) || m.RunID <= 0 || m.CandidateID <= 0 || m.AttemptID <= 0 ||
			(m.Submitted && m.ContainerID == "") || (m.Cleaned && (m.Receipt == nil || m.Receipt.Phase != "cleaned")) {
			return &Error{Code: "validator_state_error"}
		}
		seen[m.SessionID] = true
		switch m.Operation {
		case "create", "register", "attach":
		default:
			return &Error{Code: "validator_state_error"}
		}
		if m.Receipt != nil && (m.Receipt.Version != 2 || m.Receipt.SessionID != m.SessionID) {
			return &Error{Code: "validator_state_error"}
		}
	}
	return nil
}

// All registry changes run through the coordinator. A failed replacement poisons
// this invocation, including when rename succeeded before directory sync failed.
func (o *dockerOwner) save(sessions []manifest) error {
	if o.poisoned {
		return &Error{Code: "validator_state_error"}
	}
	next := registry{Version: 2, Sessions: append([]manifest{}, sessions...)}
	if o.docker.replaceJournal(next) != nil {
		o.poisoned = true
		return &Error{Code: "validator_state_error"}
	}
	o.registry = next
	return nil
}

func (o *dockerOwner) update(m manifest) error {
	entries := append([]manifest{}, o.registry.Sessions...)
	for i := range entries {
		if entries[i].SessionID == m.SessionID {
			entries[i] = m
			return o.save(entries)
		}
	}
	if len(entries) >= 4 {
		return &Error{Code: "validation_batch_stopped"}
	}
	return o.save(append(entries, m))
}

func (o *dockerOwner) forget(m manifest) error {
	entries := []manifest{}
	for _, entry := range o.registry.Sessions {
		if entry.SessionID != m.SessionID {
			entries = append(entries, entry)
		}
	}
	return o.save(entries)
}

func (o *dockerOwner) remove(ctx context.Context, m *manifest) error {
	if !m.Cleaned {
		return &Error{Code: "validation_cleanup_pending"}
	}
	ids, err := o.docker.containers(ctx, m.Endpoint)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id != m.ContainerID {
			continue
		}
		if _, err = o.docker.inspect(ctx, m); err != nil {
			return err
		}
		if _, err = o.docker.at(ctx, m.Endpoint, nil, "rm", "--force", m.ContainerID); err != nil {
			return err
		}
	}
	return nil
}

func (o *dockerOwner) reconcile(ctx context.Context) error {
	ids, err := o.docker.containers(ctx, o.endpoint)
	if err != nil {
		return err
	}
	tracked := map[string]bool{}
	entries := append([]manifest{}, o.registry.Sessions...)
	sort.Slice(
		entries, func(i, j int) bool {
			if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
				return entries[i].SessionID < entries[j].SessionID
			}
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		},
	)
	var failures []error
	for _, entry := range entries {
		m := entry
		if m.Endpoint != o.endpoint || m.DaemonID != o.daemon {
			failures = append(failures, &Error{Code: "validator_daemon_mismatch"})
			continue
		}
		if m.ContainerID != "" {
			tracked[m.ContainerID] = true
		}
		if m.ContainerID == "" {
			info, err := o.docker.inspect(ctx, &m)
			if err != nil {
				if len(ids) == 0 {
					if err = o.forget(m); err != nil {
						return err
					}
					continue
				}
				failures = append(failures, &Error{Code: "validation_cleanup_pending"})
				continue
			}
			m.ContainerID = info.ID
			tracked[m.ContainerID] = true
			if err = o.update(m); err != nil {
				return err
			}
		}
		if !m.Cleaned {
			info, err := o.docker.inspect(ctx, &m)
			if err != nil {
				failures = append(failures, &Error{Code: "validation_cleanup_pending"})
				continue
			}
			if !m.Submitted {
				m.Cleaned = true
				m.Receipt = &protocol.Receipt{Version: 2, SessionID: m.SessionID, Phase: "cleaned"}
			} else {
				if !info.State.Running {
					if _, err = o.docker.at(ctx, m.Endpoint, nil, "start", m.ContainerID); err != nil {
						failures = append(failures, err)
						continue
					}
				}
				receipt, err := o.collect(ctx, &m)
				if err != nil {
					failures = append(failures, err)
					continue
				}
				m.Cleaned = true
				m.Receipt = &receipt
			}
			if err = o.update(m); err != nil {
				return err
			}
		}
		if err = o.remove(ctx, &m); err != nil {
			failures = append(failures, err)
			continue
		}
		if err = o.forget(m); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if !tracked[id] {
			failures = append(failures, &Error{Code: "validator_orphaned_session"})
		}
	}
	return errors.Join(failures...)
}

func (o *dockerOwner) collect(ctx context.Context, m *manifest) (protocol.Receipt, error) {
	_, _ = o.docker.at(ctx, m.Endpoint, nil, "exec", m.ContainerID, "/usr/local/bin/wpc-worker", "cancel")
	for {
		receipt, err := o.docker.worker(ctx, m, "recover", nil)
		if err == nil && receipt.Phase == "cleaned" {
			return receipt, nil
		}
		var code *Error
		if !errors.As(err, &code) || code.Code != "validator_busy" {
			return receipt, &Error{Code: "validation_cleanup_pending"}
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return receipt, ctx.Err()
		case <-timer.C:
		}
	}
}

func operationInput(ctx context.Context) ([]byte, error) {
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil {
		return nil, context.DeadlineExceeded
	}
	remaining := time.Until(deadline)
	if remaining > 240*time.Second {
		remaining = 240 * time.Second
	}
	if remaining <= 0 {
		return nil, context.DeadlineExceeded
	}
	return json.Marshal(protocol.Operation{RemainingUS: remaining.Microseconds()})
}

func receiptResult(r protocol.Receipt) (Result, error) {
	result := Result{
		Outcome: r.Result.Outcome, ReasonCode: r.Result.ReasonCode, Evidence: r.Result.Evidence,
		FailureClass: r.Result.FailureClass,
	}
	if result.Outcome != state.Confirmed && result.Outcome != state.Inconclusive {
		return Result{}, &Error{Code: "validator_protocol_error"}
	}
	if result.Outcome == state.Confirmed && (result.Evidence == nil || result.Evidence.Kind != state.AccountType ||
		result.Evidence.AccountType != "Unlimited" || result.Evidence.ObservedAt.IsZero() || r.RegistrationID == "" ||
		r.DeviceID == "" || result.FailureClass != "") {
		return Result{}, &Error{Code: "validator_protocol_error"}
	}
	if result.Outcome == state.Inconclusive && result.Evidence != nil {
		return Result{}, &Error{Code: "validator_protocol_error"}
	}
	switch result.FailureClass {
	case "", state.Temporary, state.Terminal, state.Throttled, state.Unsafe:
	default:
		return Result{}, &Error{Code: "validator_protocol_error"}
	}
	return result, nil
}

func (o *dockerOwner) Run(ctx context.Context, in Input, h Hooks) (result Result, err error) {
	token := rand.Text()
	if h.Session != nil {
		if err = h.Session(token); err != nil {
			return result, err
		}
	}
	m := manifest{
		Version: 2, SessionID: token, Name: "wpc-validator-" + strings.ToLower(token), Endpoint: o.endpoint,
		DaemonID: o.daemon, Image: o.docker.Options.Image, Profile: in.StateDir, CandidateID: in.CandidateID,
		RunID:     in.RunID,
		AttemptID: in.AttemptID, CreatedAt: time.Now().UTC(), Operation: "create",
	}
	commit := func() error { snapshot := m; return h.Commit(func() error { return o.update(snapshot) }) }
	var collection context.Context
	var cancelCollection context.CancelFunc
	mutationDone := false
	registrationDone := false
	cleanupContext := func() context.Context {
		if collection == nil {
			collection, cancelCollection = h.CleanupContext()
		}
		return collection
	}
	defer func() {
		safeTimeout := errors.Is(
			err,
			context.DeadlineExceeded,
		) && (!m.Submitted || mutationDone || (m.Operation == "register" && registrationDone))
		if err != nil && !safeTimeout {
			_ = h.MutationDone(Result{FailureClass: state.Unsafe}, err)
		}
		if m.ContainerID == "" {
			return
		}
		cleanup := cleanupContext()
		defer cancelCollection()
		var receipt protocol.Receipt
		if m.Cleaned && m.Receipt != nil {
			receipt = *m.Receipt
		} else if !m.Submitted {
			receipt = protocol.Receipt{
				Version: 2, SessionID: m.SessionID, Phase: "cleaned",
				Result: protocol.Result{
					Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
				},
			}
		} else {
			var cleanupErr error
			receipt, cleanupErr = o.collect(cleanup, &m)
			if cleanupErr != nil {
				result.FailureClass = state.Unsafe
				err = errors.Join(err, cleanupErr)
				return
			}
		}
		normalized, receiptErr := receiptResult(receipt)
		if receiptErr != nil {
			err = errors.Join(err, receiptErr)
			return
		}
		result = normalized
		m.Cleaned = true
		m.Receipt = &receipt
		if cleanupErr := cleanup.Err(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
			return
		}
		if saveErr := commit(); saveErr != nil {
			err = errors.Join(err, saveErr)
			return
		}
		if cleanupErr := cleanup.Err(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
			return
		}
		result.CleanupProven = true
		if removeErr := o.remove(cleanup, &m); removeErr != nil {
			err = errors.Join(err, removeErr)
			return
		}
		if cleanupErr := cleanup.Err(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
			return
		}
		if forgetErr := h.Commit(func() error { return o.forget(m) }); forgetErr != nil {
			err = errors.Join(err, forgetErr)
			return
		}
		result.Removed = true
		if safeTimeout {
			err = nil
			result.Outcome = state.Inconclusive
			result.Evidence = nil
			result.ReasonCode = "validation_timeout"
			result.FailureClass = state.Temporary
		}
	}()
	if err = commit(); err != nil {
		return result, err
	}
	data, err := o.docker.at(
		ctx, m.Endpoint, nil, "create", "--name", m.Name, "--platform", "linux/arm64",
		"--network", "bridge", "--memory", "512m", "--cpus", "1", "--pids-limit", "256", "--security-opt",
		"no-new-privileges", "--label", containerLabel+"=1", "--label", containerLabel+".session="+m.SessionID, m.Image,
	)
	if err != nil {
		return result, err
	}
	m.ContainerID = strings.TrimSpace(string(data))
	if !idPattern.MatchString(m.ContainerID) {
		m.ContainerID = ""
		return result, &Error{Code: "validator_protocol_error"}
	}
	if err = commit(); err != nil {
		return result, err
	}
	if _, err = o.docker.inspect(ctx, &m); err != nil {
		return result, err
	}
	if _, err = o.docker.at(ctx, m.Endpoint, nil, "start", m.ContainerID); err != nil {
		return result, err
	}
	ready, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = o.docker.ready(ready, &m)
	cancel()
	if err != nil {
		return result, err
	}
	input, err := json.Marshal(protocol.Request{SessionID: m.SessionID, Key: in.Key})
	if err != nil {
		return result, err
	}
	receipt, err := o.docker.worker(ctx, &m, "initialize", input)
	if err != nil {
		return result, err
	}
	if receipt.Phase != "prepared" {
		return result, &Error{Code: "validator_protocol_error"}
	}
	if err = h.Admit(ctx, "register"); err != nil {
		return result, err
	}
	m.Submitted = true
	m.Operation = "register"
	if err = commit(); err != nil {
		_ = h.MutationDone(Result{}, err)
		return result, err
	}
	input, err = operationInput(ctx)
	if err == nil {
		receipt, err = o.docker.worker(ctx, &m, "register", input)
	}
	if err == nil && receipt.Phase != "registered" && receipt.Phase != "cleaned" {
		err = &Error{Code: "validator_protocol_error"}
	}
	if err == nil {
		result, err = receiptResult(receipt)
	}
	if gateErr := h.MutationDone(result, err); gateErr != nil {
		return result, gateErr
	}
	if err != nil {
		return result, err
	}
	if receipt.Phase == "cleaned" {
		m.Cleaned = true
		m.Receipt = &receipt
		return result, nil
	}
	registrationDone = true
	if err = h.Admit(ctx, "attach"); err != nil {
		return result, err
	}
	m.Operation = "attach"
	if err = commit(); err != nil {
		_ = h.MutationDone(Result{}, err)
		return result, err
	}
	input, err = operationInput(ctx)
	if err != nil {
		_ = h.MutationDone(Result{}, err)
		return result, err
	}
	type response struct {
		receipt protocol.Receipt
		err     error
	}
	responses := make(chan response, 1)
	// Keep the exec alive for cleanup. The host active deadline triggers cancel and
	// recovery separately, while status supplies timely observation and completion.
	execution, stopExecution := context.WithCancel(context.Background())
	defer stopExecution()
	workerManifest := m
	go func() {
		r, e := o.docker.worker(execution, &workerManifest, "attach", input)
		responses <- response{r, e}
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	defer func() {
		if !mutationDone {
			_ = h.MutationDone(Result{}, &Error{Code: "validation_batch_stopped"})
		}
	}()
	process := func(r protocol.Receipt) error {
		if r.AttachmentFinished && !mutationDone {
			if r.OperationResult == nil {
				return &Error{Code: "validator_protocol_error"}
			}
			operation := Result{
				Outcome: r.OperationResult.Outcome, ReasonCode: r.OperationResult.ReasonCode,
				FailureClass: r.OperationResult.FailureClass,
			}
			if err := h.MutationDone(operation, nil); err != nil {
				return err
			}
			mutationDone = true
		}
		if r.Phase == "observed" || r.Phase == "cleanup_pending" || r.Phase == "cleaned" {
			cleanupContext()
			normalized, err := receiptResult(r)
			if err != nil {
				return err
			}
			if err = h.Observe(normalized); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		wait := ctx
		if collection != nil {
			wait = collection
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-wait.Done():
			return result, wait.Err()
		case response := <-responses:
			if response.err != nil {
				return result, response.err
			}
			if err = process(response.receipt); err != nil {
				return result, err
			}
			if response.receipt.Phase != "cleaned" {
				return result, &Error{Code: "validation_cleanup_pending"}
			}
			m.Cleaned = true
			m.Receipt = &response.receipt
			return receiptResult(response.receipt)
		case <-tick.C:
			status, err := o.docker.worker(wait, &m, "status", nil)
			if err != nil {
				return result, err
			}
			if err = process(status); err != nil {
				return result, err
			}
		}
	}
}
