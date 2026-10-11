package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
	"github.com/stretchr/testify/require"
)

const fixtureKey = "ABCDEFGH-12345678-AbCdEf01"
const fixtureSession = "0123456789abcdefghijklmnop"

type vendor struct {
	registered     bool
	accountType    string
	license        string
	applyCode      string
	deleteCode     string
	deleteData     []byte
	retainOnDelete bool
	delay          int
	pendingKey     string
	calls          []string
	device         string
}

func (v *vendor) Run(_ context.Context, args ...string) Reply {
	op := strings.Join(args, " ")
	if len(args) > 2 && args[1] == "license" {
		op = "registration license"
	}
	v.calls = append(v.calls, op)
	switch op {
	case "registration new":
		v.registered = true
		v.license = "BASELINE-BASELINE-BASELINE"
		v.device = "device"
		return Reply{Data: []byte(`{"status":"Success"}`)}
	case "disconnect":
		return Reply{Data: []byte(`{"status":"Success"}`)}
	case "registration license":
		if v.applyCode != "" {
			return Reply{Code: v.applyCode}
		}
		v.pendingKey = args[2]
		return Reply{Data: []byte(`{"status":"Success"}`)}
	case "registration show":
		if !v.registered {
			return Reply{Code: "MissingRegistration"}
		}
		if v.pendingKey != "" {
			if v.delay == 0 {
				v.license = v.pendingKey
				v.pendingKey = ""
			} else {
				v.delay--
			}
		}
		data, _ := json.Marshal(
			map[string]any{
				"id": "registration", "device_id": v.device,
				"account": map[string]string{"id": "account", "type": v.accountType, "license": v.license},
			},
		)
		return Reply{Data: data}
	case "registration creation-status":
		return Reply{Code: "validation_provider_error"}
	case "registration delete":
		if !v.retainOnDelete {
			v.registered = false
		}
		data := v.deleteData
		if data == nil {
			data = []byte(`{"status":"Success"}`)
		}
		return Reply{Code: v.deleteCode, Data: data}
	default:
		panic("unexpected command")
	}
}

func engine(t *testing.T, v *vendor) *Engine {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	vendorDir := filepath.Join(dir, "vendor")
	require.NoError(t, os.Mkdir(vendorDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "reg.json"), []byte(`{"credential":"synthetic"}`), 0600))
	return &Engine{
		Dir: filepath.Join(dir, "worker"), VendorDir: vendorDir, Runner: v, Clock: time.Now,
		PollInterval: time.Millisecond, ObservationTimeout: 5 * time.Millisecond,
	}
}

// AC-7 and AC-8: command success alone is not entitlement, and evidence waits for cleanup.
func TestCheckRequiresAttributionAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, account, applyCode, want string
		delay                          int
		confirmed                      bool
	}{
		{"unlimited", "unlimited", "", "", 0, true},
		{"delayed", "unlimited", "", "", 1, true},
		{"free", "free", "", "not_unlimited", 0, false},
		{"capacity", "free", "ApiErrorTooManyDevices", "device_limit", 0, false},
		{"unattributed", "unlimited", "", "validation_unattributed", 100, false},
		{"unknown", "free", "VendorUnexpectedSecret", "validation_provider_error", 0, false},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				v := &vendor{accountType: tc.account, applyCode: tc.applyCode, delay: tc.delay}
				e := engine(t, v)
				r, err := e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
				require.NoError(t, err)
				require.Equal(t, "cleaned", r.Phase)
				require.False(t, v.registered)
				require.Equal(t, tc.want, r.Result.ReasonCode)
				if tc.confirmed {
					require.Equal(t, state.Confirmed, r.Result.Outcome)
					require.Equal(t, "Unlimited", r.Result.Evidence.AccountType)
				} else {
					require.Equal(t, state.Inconclusive, r.Result.Outcome)
					require.Nil(t, r.Result.Evidence)
				}
				_, err = e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
				require.ErrorIs(t, err, ErrPending)
				require.Equal(t, 1, count(v.calls, "registration new"))
				require.Equal(t, 1, count(v.calls, "registration license"))
				require.Equal(t, 1, count(v.calls, "registration delete"))
			},
		)
	}
}

func count(values []string, want string) int {
	n := 0
	for _, v := range values {
		if v == want {
			n++
		}
	}
	return n
}

// AC-7: ambiguous deletion is retained, even when local registration disappeared.
func TestLostDeletionAcknowledgementBlocksRecovery(t *testing.T) {
	v := &vendor{accountType: "unlimited", deleteCode: "validation_timeout"}
	e := engine(t, v)
	r, err := e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
	require.ErrorIs(t, err, ErrPending)
	require.Equal(t, "cleanup_pending", r.Phase)
	saved, err := e.load()
	require.NoError(t, err)
	require.NotNil(t, saved.Result.Evidence)
	_, err = e.Recover(context.Background())
	require.ErrorIs(t, err, ErrPending)
	require.Equal(t, 1, count(v.calls, "registration new"))
	require.Equal(t, 1, count(v.calls, "registration license"))
	require.Equal(t, 1, count(v.calls, "registration delete"))
	data, err := os.ReadFile(filepath.Join(e.Dir, "registration-backup", "reg.json"))
	require.NoError(t, err)
	require.Contains(t, string(data), "synthetic")
}

// AC-7: a restarted worker only reconciles the known resource, never reattaches.
func TestRecoveryStages(t *testing.T) {
	for _, phase := range []string{
		"prepared", "registration_pending", "registered", "attachment_pending", "observed",
		"cleanup_pending", "cleaned",
	} {
		t.Run(
			phase, func(t *testing.T) {
				v := &vendor{registered: true, device: "device", accountType: "free", license: fixtureKey}
				e := engine(t, v)
				require.NoError(t, privatefs.Directory(e.Dir))
				s := protocol.Session{
					Receipt: protocol.Receipt{
						Version: 1, SessionID: fixtureSession, RegistrationID: "registration", DeviceID: "device",
					}, Key: fixtureKey,
				}
				if phase == "prepared" || phase == "registration_pending" {
					s.RegistrationID = ""
					s.DeviceID = ""
				}
				require.NoError(t, e.save(&s, phase))
				r, err := e.Recover(context.Background())
				require.NoError(t, err)
				require.Equal(t, "cleaned", r.Phase)
				require.NotContains(t, v.calls, "registration new")
				require.NotContains(t, v.calls, "registration license")
				if phase != "prepared" && phase != "cleaned" {
					require.False(t, v.registered)
				}
			},
		)
	}
	for _, phase := range []string{"registration_pending", "cleanup_pending"} {
		t.Run(
			phase+"_missing", func(t *testing.T) {
				v := &vendor{accountType: "free"}
				e := engine(t, v)
				require.NoError(t, privatefs.Directory(e.Dir))
				s := protocol.Session{Receipt: protocol.Receipt{Version: 1, SessionID: fixtureSession}, Key: fixtureKey}
				if phase == "cleanup_pending" {
					s.RegistrationID = "registration"
					s.DeviceID = "device"
				}
				require.NoError(t, e.save(&s, phase))
				_, err := e.Recover(context.Background())
				require.ErrorIs(t, err, ErrPending)
				require.NotContains(t, v.calls, "registration new")
				require.NotContains(t, v.calls, "registration license")
			},
		)
	}
}

// AC-7: journal failures happen before the corresponding remote mutation.
func TestJournalFailureStopsMutation(t *testing.T) {
	v := &vendor{accountType: "unlimited"}
	e := engine(t, v)
	e.afterSave = func(phase string) error {
		if phase == "attachment_pending" {
			return errors.New("simulated process stop")
		}
		return nil
	}
	_, err := e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
	require.Error(t, err)
	require.NotContains(t, v.calls, "registration license")
	require.True(t, v.registered)
	e.afterSave = nil
	r, err := e.Recover(context.Background())
	require.NoError(t, err)
	require.Equal(t, "cleaned", r.Phase)
	require.False(t, v.registered)
}

func TestCancellationPreventsAttachment(t *testing.T) {
	v := &vendor{accountType: "unlimited"}
	e := engine(t, v)
	e.afterSave = func(phase string) error {
		if phase == "registered" {
			return e.Cancel()
		}
		return nil
	}
	r, err := e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
	require.NoError(t, err)
	require.Equal(t, "cancelled", r.Result.ReasonCode)
	require.Equal(t, "cleaned", r.Phase)
	require.NotContains(t, v.calls, "registration license")
}

func TestUnknownDeletionPayloadDoesNotProveCleanup(t *testing.T) {
	for _, payload := range []string{`{}`, `null`, `{"status":"Unknown"}`, `{"error":"failed"}`} {
		t.Run(
			payload, func(t *testing.T) {
				v := &vendor{accountType: "unlimited", deleteData: []byte(payload)}
				e := engine(t, v)
				r, err := e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
				require.ErrorIs(t, err, ErrPending)
				require.Equal(t, "cleanup_pending", r.Phase)
			},
		)
	}
}

func TestRecoveryPreservesPrivateObservationOnAnotherFailure(t *testing.T) {
	v := &vendor{accountType: "unlimited", deleteCode: "validation_timeout", retainOnDelete: true}
	e := engine(t, v)
	_, err := e.Check(context.Background(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
	require.ErrorIs(t, err, ErrPending)
	_, err = e.Recover(context.Background())
	require.ErrorIs(t, err, ErrPending)
	s, err := e.load()
	require.NoError(t, err)
	require.Equal(t, state.Confirmed, s.Result.Outcome)
	require.NotNil(t, s.Result.Evidence)
	require.Equal(t, "Unlimited", s.Result.Evidence.AccountType)
}

func TestInitializedRequestCannotStartAfterRecovery(t *testing.T) {
	v := &vendor{accountType: "unlimited"}
	e := engine(t, v)
	_, err := e.Initialize(protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
	require.NoError(t, err)
	require.Empty(t, v.calls)
	r, err := e.Recover(context.Background())
	require.NoError(t, err)
	require.Equal(t, "cleaned", r.Phase)
	_, err = e.Start(context.Background())
	require.ErrorIs(t, err, ErrPending)
	require.Empty(t, v.calls)
}

func TestRateLimitProofMustBeCompleteAndUnique(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	failure := "api_request{request_id=" + id + " request=new_registration}: Failed API Request err=ApiFailure { " +
		"code: 429, headers: redacted }"
	terminal := "api_request{request_id=" + id + " request=new_registration}: Request completed in 26ms " +
		"result=Err(MaxRateLimitAttemptsReached(0)) " +
		"rate_limit_status=RateLimitStatus { is_rate_limited: true }"
	require.True(t, registrationRejected([]byte(failure+"\n"+terminal)))
	ambiguous := "api_request{request_id=" + id + " request=new_registration}: Failed API Request err=TransportError"
	conflict := "api_request{request_id=" + id + " request=new_registration}: Request completed result=Err(Timeout)"
	for _, input := range []string{
		ambiguous + "\n" + failure + "\n" + terminal, failure + "\n" + terminal + "\n" + conflict,
		terminal + "\n" + failure, failure + "\n" + failure + "\n" + terminal,
	} {
		require.False(t, registrationRejected([]byte(input)))
	}
	for _, input := range []string{
		failure, terminal, strings.ReplaceAll(failure+"\n"+terminal, "429", "500"),
		failure + "\n" + strings.ReplaceAll(terminal, id, "aaaaaaaa-89ab-cdef-0123-456789abcdef"),
		failure + "\n" + terminal + "\napi_request{request_id=" + id +
			" request=new_registration}: Request completed result=Ok",
	} {
		require.False(t, registrationRejected([]byte(input)))
	}
}

// covers: spec 0003 AC-6, AC-7. Invalid requests have no filesystem or vendor effects.
func TestInitializeRejectsInvalidIdentityBeforeMutation(t *testing.T) {
	for _, in := range []protocol.Request{
		{SessionID: "short", Key: fixtureKey},
		{SessionID: "../../private-secret", Key: fixtureKey},
		{SessionID: fixtureSession, Key: "private-secret"},
		{SessionID: fixtureSession, Key: fixtureKey + "extra"},
	} {
		v := &vendor{}
		e := engine(t, v)
		_, err := e.Initialize(in)
		require.EqualError(t, err, "validation_invalid_input")
		require.Empty(t, v.calls)
		_, err = os.Stat(e.Dir)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

// covers: spec 0003 AC-7. Recovery refuses to delete a different remote device.
func TestRecoveryRejectsChangedRegistrationIdentity(t *testing.T) {
	v := &vendor{registered: true, accountType: "unlimited", license: fixtureKey, device: "foreign-device"}
	e := engine(t, v)
	require.NoError(t, privatefs.Directory(e.Dir))
	s := protocol.Session{
		Receipt: protocol.Receipt{
			Version: 1, SessionID: fixtureSession, RegistrationID: "registration", DeviceID: "original-device",
		}, Key: fixtureKey,
	}
	require.NoError(t, e.save(&s, "attachment_pending"))
	_, err := e.Recover(t.Context())
	require.ErrorIs(t, err, ErrPending)
	require.True(t, v.registered)
	require.NotContains(t, v.calls, "registration delete")
	require.NotContains(t, v.calls, "registration license")
	require.NotContains(t, v.calls, "registration new")
	receipt, err := e.Status()
	require.NoError(t, err)
	require.Equal(t, "attachment_pending", receipt.Phase)
}
