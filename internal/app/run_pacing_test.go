package app_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

// Spec 0007 AC-2: consecutive scheduled batches retain the configured gap
// between the last mutation in one batch and the first mutation in the next.
func TestRunPreservesMutationPacingAcrossBatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := batchApp(t, 3)
		scheduledSettings(a)
		a.Settings.CheckPolicy.Workers = 1
		type observedMutation struct {
			runID int64
			at    time.Time
		}
		var mutations []observedMutation
		fixture := &scheduledFixture{}
		fixture.run = func(ctx context.Context, in validator.Input, hooks validator.Hooks) (validator.Result, error) {
			for _, operation := range []string{"register", "attach"} {
				if err := hooks.Admit(ctx, operation); err != nil {
					return validator.Result{}, err
				}
				mutations = append(mutations, observedMutation{in.RunID, time.Now()})
				if err := hooks.MutationDone(validator.Result{}, nil); err != nil {
					return validator.Result{}, err
				}
			}
			return validator.Result{
				Outcome: state.Inconclusive, ReasonCode: "controlled_terminal", FailureClass: state.Terminal,
				CleanupProven: true, Removed: true,
			}, nil
		}
		a.Validator = fixture
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		selected := 0
		err := a.Run(ctx, func(summary app.RunSummary) error {
			selected += summary.Validation.Totals.Selected
			if selected == 3 {
				cancel()
			}
			return nil
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, mutations, 6)
		require.NotEqual(t, mutations[3].runID, mutations[4].runID)
		for i := 1; i < len(mutations); i++ {
			require.GreaterOrEqual(t, mutations[i].at.Sub(mutations[i-1].at), time.Second,
				"mutation %d, previous run %d, next run %d", i, mutations[i-1].runID, mutations[i].runID)
		}
	})
}
