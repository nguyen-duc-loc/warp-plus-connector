package validator

import (
	"context"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/stretchr/testify/require"
)

// Both a rejected command and a lost command acknowledgement stop staged work.
// Recovery uses retained identities and never repeats registration or attachment.
func TestStagedDockerCommandFailuresRecoverWithoutNewMutations(t *testing.T) {
	for _, operation := range []string{
		"create", "start", "ready", "initialize", "register", "attach", "status", "recover", "rm",
	} {
		for _, acknowledged := range []bool{false, true} {
			when := map[bool]string{false: "before effect", true: "after effect"}[acknowledged]
			t.Run(operation+"/"+when, func(t *testing.T) {
				d, f := dockerFixture(t)
				owner, err := d.Acquire(t.Context())
				require.NoError(t, err)
				injected := false
				attached := make(chan struct{})
				released := make(chan struct{})
				var mutations []string
				d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
					op := args[2]
					if op == "exec" {
						op = args[len(args)-1]
					}
					if operation == "status" && op == "status" {
						<-attached
					}
					fail := op == operation && !injected
					failure := &Error{Code: "validator_runtime_error"}
					if op == "ready" {
						failure.Code = "validator_version_mismatch"
					}
					if fail {
						injected = true
						if !acknowledged {
							return nil, failure
						}
					}
					if op == "register" || op == "attach" {
						require.False(t, injected && !fail, "a failure cannot authorize another mutation")
						mutations = append(mutations, op)
					}
					data, err := f.run(ctx, input, args...)
					if fail {
						return nil, failure
					}
					if op == "attach" && operation == "recover" {
						// Force receipt collection so its own failure boundary is reached.
						return nil, &Error{Code: "validator_runtime_error"}
					}
					if op == "attach" && operation == "status" {
						close(attached)
						<-ctx.Done()
						close(released)
						return nil, ctx.Err()
					}
					return data, err
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				_, err = owner.Run(ctx,
					Input{StateDir: "/profile", RunID: 1, CandidateID: 1, AttemptID: 1}, fixtureHooks())
				require.Error(t, err)
				require.True(t, injected)
				if operation == "status" {
					select {
					case <-released:
					case <-time.After(time.Second):
						t.Fatal("attachment command did not stop")
					}
				}
				creates, checks := f.creates, f.checks
				for _, mutation := range []string{"register", "attach"} {
					count := 0
					for _, command := range mutations {
						if command == mutation {
							count++
						}
					}
					require.LessOrEqual(t, count, 1)
				}
				var saved registry
				require.NoError(t, privatefs.Read(d.path(), &saved))
				if operation == "recover" {
					require.Len(t, saved.Sessions, 1)
					require.Equal(t, f.session, saved.Sessions[0].SessionID)
					require.Equal(t, testContainer, saved.Sessions[0].ContainerID)
					require.False(t, saved.Sessions[0].Cleaned)
					require.Zero(t, f.removals)
				}
				require.NoError(t, owner.Close())
				beforeRecovery := append([]string{}, mutations...)
				require.NoError(t, d.Recover(t.Context()))
				require.Equal(t, beforeRecovery, append([]string{}, mutations...))
				require.Equal(t, creates, f.creates)
				require.Equal(t, checks, f.checks)
				require.Equal(t, creates, f.removals)
				require.NoError(t, privatefs.Read(d.path(), &saved))
				require.Empty(t, saved.Sessions)
				require.Empty(t, f.ids)
			})
		}
	}
}
