// Package worker owns a single isolated WARP registration and its recovery journal.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
)

// ErrPending prevents new work when remote cleanup is not proven.
var ErrPending = errors.New("validation_cleanup_pending")

// Reply is bounded vendor output that never reaches application diagnostics.
type Reply struct {
	Code string
	Data []byte
}

// Runner isolates the external vendor process boundary.
type Runner interface {
	Run(context.Context, ...string) Reply
}

// Engine coordinates one private worker journal with the isolated vendor daemon.
type Engine struct {
	Dir                string
	VendorDir          string
	Runner             Runner
	Clock              func() time.Time
	PollInterval       time.Duration
	ObservationTimeout time.Duration
	afterSave          func(string) error
}

type registration struct {
	ID       string `json:"id"`
	DeviceID string `json:"device_id"`
	Account  struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		License string `json:"license"`
	} `json:"account"`
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9]{8}-[A-Za-z0-9]{8}-[A-Za-z0-9]{8}$`)
var sessionPattern = regexp.MustCompile(`^[a-zA-Z0-9]{16,64}$`)

func (e *Engine) journal() string { return filepath.Join(e.Dir, "session.json") }

func (e *Engine) save(s *protocol.Session, phase string) error {
	s.Phase, s.UpdatedAt = phase, e.Clock().UTC()
	if err := privatefs.Write(e.journal(), s); err != nil {
		return err
	}
	if e.afterSave != nil {
		return e.afterSave(phase)
	}
	return nil
}

func (e *Engine) load() (protocol.Session, error) {
	var s protocol.Session
	if err := privatefs.Read(e.journal(), &s); err != nil {
		return s, err
	}
	if s.Version != protocol.Version || !sessionPattern.MatchString(s.SessionID) || !keyPattern.MatchString(s.Key) {
		return s, privatefs.ErrAccess
	}
	switch s.Phase {
	case "prepared", "registration_pending", "registration_created":
		if s.RegistrationID != "" || s.DeviceID != "" {
			return s, privatefs.ErrAccess
		}
	case "registration_identified", "registered", "attachment_pending", "observed", "cleanup_pending":
		if s.RegistrationID == "" || s.DeviceID == "" {
			return s, privatefs.ErrAccess
		}
	case "cleaned":
		if (s.RegistrationID == "") != (s.DeviceID == "") {
			return s, privatefs.ErrAccess
		}
		if s.RegistrationID == "" && s.Result.Evidence != nil {
			return s, privatefs.ErrAccess
		}
	default:
		return s, privatefs.ErrAccess
	}
	return s, nil
}

func (e *Engine) lock() (*os.File, error) {
	if err := privatefs.Directory(e.Dir); err != nil {
		return nil, err
	}
	return privatefs.Lock(filepath.Join(e.Dir, "worker.lock"))
}

// Initialize durably records input without performing any remote operation.
func (e *Engine) Initialize(in protocol.Request) (protocol.Receipt, error) {
	var zero protocol.Receipt
	if !sessionPattern.MatchString(in.SessionID) || !keyPattern.MatchString(in.Key) {
		return zero, errors.New("validation_invalid_input")
	}
	lock, err := e.lock()
	if err != nil {
		return zero, err
	}
	defer func() { _ = lock.Close() }()
	if _, err := os.Lstat(e.journal()); !errors.Is(err, os.ErrNotExist) {
		return zero, ErrPending
	}
	s := protocol.Session{
		Receipt: protocol.Receipt{
			Version: protocol.Version, SessionID: in.SessionID,
			Result: protocol.Result{Outcome: state.Inconclusive, ReasonCode: "validation_interrupted"},
		}, Key: in.Key,
	}
	if err := e.save(&s, "prepared"); err != nil {
		return s.Receipt, err
	}
	return s.Receipt, nil
}

func (e *Engine) cancelled(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	var marker bool
	err := privatefs.Read(filepath.Join(e.Dir, "cancel.json"), &marker)
	return !errors.Is(err, os.ErrNotExist)
}

// operationContext observes cancellation while a vendor command is running.
func (e *Engine) operationContext(parent context.Context, in protocol.Operation) (
	context.Context,
	context.CancelFunc,
	error,
) {
	if in.RemainingUS <= 0 || in.RemainingUS > 240000000 {
		return nil, nil, errors.New("validation_invalid_input")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(in.RemainingUS)*time.Microsecond)
	if e.cancelled(ctx) {
		cancel()
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e.cancelled(ctx) {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel, nil
}

// Register creates and identifies one registration, then awaits host attachment.
func (e *Engine) Register(parent context.Context, in protocol.Operation) (protocol.Receipt, error) {
	lock, err := e.lock()
	if err != nil {
		return protocol.Receipt{}, err
	}
	defer func() { _ = lock.Close() }()
	s, err := e.load()
	if err != nil {
		return s.Receipt, err
	}
	if s.Phase != "prepared" {
		return s.Receipt, ErrPending
	}
	ctx, cancel, err := e.operationContext(parent, in)
	if err != nil {
		return s.Receipt, err
	}
	defer cancel()
	if !e.cancelled(ctx) {
		if err = e.save(&s, "registration_pending"); err != nil {
			return s.Receipt, err
		}
	}
	if e.cancelled(ctx) {
		s.Result.ReasonCode = "cancelled"
		s.Result.FailureClass = state.Terminal
		err = e.save(&s, "cleaned")
		return s.Receipt, err
	}
	if reply := e.Runner.Run(ctx, "registration", "new"); reply.Code != "" {
		if e.rejectedRegistration(ctx) {
			s.Result.ReasonCode = "validation_rate_limited"
			s.Result.FailureClass = state.Throttled
			err = e.save(&s, "cleaned")
			return s.Receipt, err
		}
		return s.Receipt, ErrPending
	}
	if err = e.save(&s, "registration_created"); err != nil {
		return s.Receipt, err
	}
	r, err := e.registration(ctx)
	if err != nil {
		return s.Receipt, ErrPending
	}
	s.RegistrationID, s.DeviceID = r.ID, r.DeviceID
	if err = e.save(&s, "registration_identified"); err != nil {
		return s.Receipt, err
	}
	if e.cancelled(ctx) || e.Runner.Run(ctx, "disconnect").Code != "" {
		return s.Receipt, ErrPending
	}
	s.Result.FailureClass = ""
	err = e.save(&s, "registered")
	return s.Receipt, err
}

// Attach applies the license once, retains command completion, observes, and cleans.
func (e *Engine) Attach(parent context.Context, in protocol.Operation) (protocol.Receipt, error) {
	lock, err := e.lock()
	if err != nil {
		return protocol.Receipt{}, err
	}
	defer func() { _ = lock.Close() }()
	s, err := e.load()
	if err != nil {
		return s.Receipt, err
	}
	if s.Phase != "registered" {
		return s.Receipt, ErrPending
	}
	ctx, cancel, err := e.operationContext(parent, in)
	if err != nil {
		return s.Receipt, err
	}
	err = e.attach(ctx, &s)
	cancel()
	if err != nil {
		return s.Receipt, err
	}
	cleanup, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	err = e.cleanup(cleanup, &s)
	return s.Receipt, err
}

func (e *Engine) attach(ctx context.Context, s *protocol.Session) error {
	if !e.cancelled(ctx) {
		if err := e.save(s, "attachment_pending"); err != nil {
			return err
		}
	}
	if e.cancelled(ctx) {
		s.Result.ReasonCode = "cancelled"
		s.Result.FailureClass = state.Terminal
		s.AttachmentFinished = true
		operation := s.Result
		s.OperationResult = &operation
		return e.save(s, "observed")
	}
	reply := e.Runner.Run(ctx, "registration", "license", s.Key)
	s.AttachmentFinished = true
	operation := protocol.Result{Outcome: state.Inconclusive}
	if reply.Code != "" {
		operation.ReasonCode = reason(reply.Code)
		operation.FailureClass = state.Terminal
		switch operation.ReasonCode {
		case "validation_timeout":
			operation.FailureClass = state.Temporary
		case "rate_limited":
			operation.ReasonCode = "validation_rate_limited"
			operation.FailureClass = state.Throttled
		}
	}
	s.OperationResult = &operation
	if err := e.save(s, "attachment_pending"); err != nil {
		return err
	}
	if reply.Code != "" {
		s.Result = operation
		return e.save(s, "observed")
	}
	interval := e.PollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	limit := e.ObservationTimeout
	if limit <= 0 {
		limit = 10 * time.Second
	}
	poll, stop := context.WithTimeout(ctx, limit)
	defer stop()
	s.Result = protocol.Result{
		Outcome: state.Inconclusive, ReasonCode: "validation_observation_timeout", FailureClass: state.Temporary,
	}
	for {
		if e.cancelled(ctx) {
			s.Result.ReasonCode = "cancelled"
			s.Result.FailureClass = state.Terminal
			break
		}
		r, err := e.registration(poll)
		if err != nil {
			s.Result.ReasonCode = "validation_unattributed"
			s.Result.FailureClass = state.Terminal
			break
		}
		if r.ID != s.RegistrationID || r.DeviceID != s.DeviceID {
			return ErrPending
		}
		if r.Account.License == s.Key {
			s.Result.ReasonCode = "not_unlimited"
			s.Result.FailureClass = state.Terminal
			if r.Account.Type == "unlimited" {
				s.Result = protocol.Result{
					Outcome: state.Confirmed, Evidence: &state.Evidence{
						Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: e.Clock().UTC(),
					},
				}
			}
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-poll.Done():
			timer.Stop()
		case <-timer.C:
		}
		if poll.Err() != nil {
			break
		}
	}
	return e.save(s, "observed")
}

func reason(code string) string {
	switch code {
	case "ApiErrorTooManyDevices":
		return "device_limit"
	case "ApiErrorTooManyRequests", "ApiErrorRateLimited":
		return "rate_limited"
	case "validation_timeout":
		return "validation_timeout"
	default:
		return "validation_provider_error"
	}
}

func (e *Engine) registration(ctx context.Context) (registration, error) {
	var r registration
	reply := e.Runner.Run(ctx, "registration", "show")
	if reply.Code != "" {
		return r, ErrPending
	}
	if json.Unmarshal(
		reply.Data,
		&r,
	) != nil || r.ID == "" || r.DeviceID == "" || r.Account.ID == "" || r.Account.Type == "" ||
		r.Account.License == "" {
		return r, ErrPending
	}
	return r, nil
}

func (e *Engine) backup() error {
	target := filepath.Join(e.Dir, "registration-backup")
	if err := privatefs.Directory(target); err != nil {
		return err
	}
	var total int64
	err := filepath.WalkDir(
		e.VendorDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return privatefs.ErrAccess
			}
			rel, err := filepath.Rel(e.VendorDir, path)
			if err != nil {
				return privatefs.ErrAccess
			}
			if d.IsDir() {
				return privatefs.Directory(filepath.Join(target, rel))
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				return privatefs.ErrAccess
			}
			total += info.Size()
			if total > 64*1024*1024 {
				return privatefs.ErrAccess
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return privatefs.ErrAccess
			}
			return privatefs.WriteBytes(filepath.Join(target, rel), data)
		},
	)
	if err != nil {
		return privatefs.ErrAccess
	}
	return nil
}

func (e *Engine) cleanup(ctx context.Context, s *protocol.Session) error {
	if s.Phase == "cleaned" {
		return nil
	}
	if s.Phase == "prepared" {
		return e.save(s, "cleaned")
	}
	if s.Phase == "registration_pending" && s.RegistrationID == "" && e.rejectedRegistration(ctx) {
		s.Result.ReasonCode = "validation_rate_limited"
		s.Result.FailureClass = state.Throttled
		return e.save(s, "cleaned")
	}
	r, err := e.registration(ctx)
	if err != nil {
		return ErrPending
	} // Missing local state never proves remote deletion.
	if s.RegistrationID != "" && (s.RegistrationID != r.ID || s.DeviceID != r.DeviceID) {
		return ErrPending
	}
	if s.RegistrationID == "" {
		s.RegistrationID, s.DeviceID = r.ID, r.DeviceID
	}
	if s.Phase != "cleanup_pending" {
		if err := e.backup(); err != nil {
			return err
		}
	}
	if err := e.save(s, "cleanup_pending"); err != nil {
		return err
	}
	if reply := e.Runner.Run(ctx, "registration", "delete"); reply.Code != "" || !acknowledged(reply.Data) {
		return ErrPending
	}
	if reply := e.Runner.Run(ctx, "registration", "show"); reply.Code != "MissingRegistration" {
		return ErrPending
	}
	return e.save(s, "cleaned")
}

func (e *Engine) rejectedRegistration(ctx context.Context) bool {
	return e.Runner.Run(ctx, "registration", "creation-status").Code == "registration_rate_limited" && e.Runner.Run(
		ctx,
		"registration",
		"show",
	).Code == "MissingRegistration"
}

// Recover reconciles the existing registration without ever creating or attaching.
func (e *Engine) Recover(ctx context.Context) (protocol.Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var zero protocol.Receipt
	lock, err := e.lock()
	if err != nil {
		return zero, err
	}
	defer func() { _ = lock.Close() }()
	s, err := e.load()
	if err != nil {
		return zero, err
	}
	err = e.cleanup(ctx, &s)
	return s.Receipt, err
}

// Status reads the durable receipt while another worker may be running.
func (e *Engine) Status() (protocol.Receipt, error) { s, err := e.load(); return s.Receipt, err }

// Cancel asks a surviving worker to stop before its next attachment and clean up.
func (e *Engine) Cancel() error {
	if err := privatefs.Directory(e.Dir); err != nil {
		return err
	}
	return privatefs.Write(filepath.Join(e.Dir, "cancel.json"), true)
}
