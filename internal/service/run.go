package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

type lifecycleKey struct{}
type lifecycle struct {
	started, shutdown func()
	signalCode        func() int
}

// WithLifecycle connects managed execution to process exit and watchdog ownership.
func WithLifecycle(ctx context.Context, started, shutdown func(), signalCode func() int) context.Context {
	return context.WithValue(ctx, lifecycleKey{}, lifecycle{started, shutdown, signalCode})
}

// RunManaged enters the existing coordinator under a separate service lifetime owner.
func RunManaged(parent context.Context, id, version string) (err error) {
	if !identityPattern.MatchString(id) {
		return failure("invalid_arguments")
	}
	hooks, _ := parent.Value(lifecycleKey{}).(lifecycle)
	if hooks.started != nil {
		hooks.started()
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	done := make(chan struct{})
	defer close(done)
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(
			func() {
				if hooks.shutdown != nil {
					hooks.shutdown()
				}
			},
		)
	}
	go func() {
		select {
		case <-ctx.Done():
			shutdown()
		case <-done:
		}
	}()
	a, err := ResolveAccount(ctx)
	if err != nil {
		return err
	}
	if err := checkDirectory(a.Root); err != nil {
		return err
	}
	lock, err := runnerLock(filepath.Join(a.Root, "runner.lock"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	m, err := a.manifest()
	if err != nil {
		return err
	}
	if m.InstallationID != id || m.Phase != "ready" {
		return failure("service_operation_incomplete")
	}
	invocation, err := newIdentity()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	o := Observation{
		SchemaVersion: 1, InstallationID: id, InvocationID: invocation, PID: os.Getpid(),
		StartedAt: now, UpdatedAt: now, Phase: "starting", CrawlPhase: "unknown", ValidationPhase: "unknown",
	}
	p := newPublisher(a.Root, o, cancel)
	defer func() { err = errors.Join(err, p.close()) }()
	if err := p.publish(context.Background(), nil); err != nil {
		shutdown()
		return err
	}
	archive, err := openArchive(a.Root, id, invocation)
	if err != nil {
		shutdown()
		_ = p.publish(context.Background(), func(o *Observation) { terminal(o, "service_log_failed", 1) })
		return err
	}
	defer func() { err = errors.Join(err, archive.close()) }()
	log := func(e Event) error {
		if err := archive.append(e); err != nil {
			cancel(err)
			shutdown()
			return err
		}
		return nil
	}
	if err := log(Event{Type: "runner_phase", Status: "starting"}); err != nil {
		return err
	}
	beatDone := make(chan struct{})
	beatStop := make(chan struct{})
	go func() {
		defer close(beatDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		stopSignal := ctx.Done()
		for {
			select {
			case <-ticker.C:
				_ = p.publish(context.Background(), nil)
			case <-stopSignal:
				stopSignal = nil
				shutdown()
				_ = p.publish(
					context.Background(), func(o *Observation) {
						if !o.terminal() {
							o.Phase = "stopping"
							o.CrawlPhase = "stopping"
							o.ValidationPhase = "stopping"
						}
					},
				)
			case <-beatStop:
				return
			}
		}
	}()
	defer func() { close(beatStop); <-beatDone }()
	finish := func(final context.Context, runErr error) error {
		if runErr != nil {
			shutdown()
		}
		code, exit := "", 0
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			code, exit = stableCode(runErr), 1
		}
		if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
			code, exit = stableCode(cause), 1
		}
		if parent.Err() != nil && code == "" {
			exit = 143
			if hooks.signalCode != nil {
				exit = hooks.signalCode()
			}
		}
		if err := p.publish(final, func(o *Observation) { terminal(o, code, exit) }); err != nil {
			return err
		}
		phase := "stopped"
		if code != "" {
			phase = "failed"
		}
		if err := log(Event{Type: "runner_phase", Status: phase, Code: code}); err != nil {
			_ = p.publish(final, func(o *Observation) { terminal(o, "service_log_failed", 1) })
			return err
		}
		return runErr
	}
	settings, err := m.Settings()
	if err != nil {
		return finish(context.Background(), err)
	}
	if err := p.publish(
		context.Background(), func(o *Observation) {
			o.CrawlEnabled, o.ValidationEnabled = &settings.CrawlEnabled, &settings.ValidationEnabled
			o.Policy = &Policy{
				CrawlInterval: settings.CrawlInterval.String(), RecheckInterval: settings.RecheckInterval.String(),
				FailureInterval: settings.FailureInterval.String(), Workers: settings.CheckPolicy.Workers,
			}
		},
	); err != nil {
		return finish(context.Background(), err)
	}
	var backend validator.Backend = validator.Unsupported{Version: version}
	if settings.ValidationEnabled {
		backend = &validator.Docker{
			Options: validator.DockerOptions{
				Image:  settings.ValidatorImage,
				Socket: *m.DockerSocket, Executable: *m.DockerExecutable, StateDir: settings.ValidatorStateDir,
			},
		}
	}
	last := app.RunnerState{Phase: "starting", CrawlPhase: "unknown", ValidationPhase: "unknown"}
	coordinator := app.App{
		Settings: settings, Source: telegram.New(version, time.Now, nil), Validator: backend,
		Clock: time.Now, Logger: slog.New(&managedHandler{emit: log}),
	}
	coordinator.Observe = func(s app.RunnerState) error {
		if s == last {
			return nil
		}
		if err := p.publish(
			context.Background(), func(o *Observation) {
				// A cancellation heartbeat must never be overwritten by a late active callback.
				if o.Phase == "stopping" && s.Phase != "stopping" {
					return
				}
				o.Phase, o.CrawlPhase, o.ValidationPhase = s.Phase, s.CrawlPhase, s.ValidationPhase
			},
		); err != nil {
			return err
		}
		if s.Phase != last.Phase {
			if err := log(Event{Type: "runner_phase", Status: s.Phase}); err != nil {
				return err
			}
		}
		for _, lane := range []struct{ name, before, after string }{
			{"crawl", last.CrawlPhase, s.CrawlPhase}, {"validation", last.ValidationPhase, s.ValidationPhase},
		} {
			if lane.before != lane.after {
				if err := log(Event{Type: "lane_phase", Lane: lane.name, Status: lane.after}); err != nil {
					return err
				}
			}
		}
		last = s
		return nil
	}
	coordinator.Finalize = func(final context.Context, runErr error) error {
		e := finish(final, runErr)
		if e == runErr {
			return nil
		}
		return e
	}
	return coordinator.Run(ctx, func(summary app.RunSummary) error { return emitSummary(log, summary) })
}

func terminal(o *Observation, code string, exit int) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	o.Phase, o.StoppedAt, o.AppExitCode = "stopped", &now, &exit
	if code != "" {
		o.Phase, o.FailureCode = "failed", &code
	}
}

func stableCode(err error) string {
	var service *Error
	if errors.As(err, &service) {
		return service.Code
	}
	var application *app.Error
	if errors.As(err, &application) {
		return application.Code
	}
	var validation *validator.Error
	if errors.As(err, &validation) {
		return validation.Code
	}
	return managedCode(err)
}

func codeValue(code *string) string {
	if code == nil {
		return ""
	}
	return *code
}

// ValidationCounts retains the saved totals using the public diagnostic field names.
func ValidationCounts(t state.ValidationTotals) map[string]int64 {
	return map[string]int64{
		"selected": int64(t.Selected), "pending": int64(t.Pending), "active": int64(t.Active),
		"completed": int64(t.Completed), "skipped": int64(t.Skipped), "interrupted": int64(t.Interrupted),
		"confirmed": int64(t.Confirmed), "invalid": int64(t.Invalid), "inconclusive": int64(t.Inconclusive),
		"unresolved": int64(t.Unresolved), "unstarted": int64(t.Unstarted),
	}
}

func emitSummary(emit func(Event) error, s app.RunSummary) error {
	if r := s.Crawl; r != nil {
		if err := emit(
			Event{
				Type: "crawl_result", RunID: r.Run.ID, Origin: r.Run.Origin, Status: string(r.Run.Status),
				Code: codeValue(r.Run.ReasonCode), Counts: map[string]int64{
					"saved_candidates": r.SavedCandidates, "saved_sightings": r.SavedSightings,
				},
			},
		); err != nil {
			return err
		}
		for _, source := range r.Sources {
			if err := emit(
				Event{
					Type: "source_result", RunID: r.Run.ID, Channel: source.Channel,
					Status: string(source.Status), Code: codeValue(source.StopReason), Counts: map[string]int64{
						"saved_candidates": source.SavedCandidates, "saved_sightings": source.SavedSightings,
					},
				},
			); err != nil {
				return err
			}
		}
	} else if r := s.Validation; r != nil {
		return emit(
			Event{
				Type: "validation_result", RunID: r.Run.ID, Origin: r.Run.Origin,
				Status: string(r.Run.Status), Code: codeValue(r.Run.ReasonCode), Counts: ValidationCounts(r.Totals),
			},
		)
	}
	return nil
}

type managedHandler struct{ emit func(Event) error }

func (h *managedHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *managedHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "run" {
		return nil
	}
	e := Event{Type: "work_diagnostic"}
	r.Attrs(
		func(a slog.Attr) bool {
			switch a.Key {
			case "run_id":
				if a.Value.Kind() == slog.KindInt64 {
					e.RunID = a.Value.Int64()
				}
			case "result":
				if a.Value.Kind() == slog.KindString {
					e.Code = a.Value.String()
				}
			}
			return true
		},
	)
	return h.emit(e)
}
func (h *managedHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *managedHandler) WithGroup(string) slog.Handler      { return h }
