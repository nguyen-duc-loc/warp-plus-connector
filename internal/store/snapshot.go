package store

import (
	"context"
	"database/sql"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// VisitCandidates reads summaries in ID order from one fixed database snapshot.
// Returning false stops normally. Callback errors retain their identity. Callbacks
// must not reenter this reader. Cancellation waits for entered callbacks to finish.
func (r *Reader) VisitCandidates(
	ctx context.Context, evaluation state.Evaluation, visit func(state.Candidate) (bool, error),
) error {
	e, err := normalizeEvaluation(evaluation)
	if err != nil || visit == nil {
		return ErrInvalidInput
	}
	var callbackErr error
	_, err = transactOwned(
		ctx, r, false, "visit candidates", true, func(tx *sql.Tx) (struct{}, error) {
			request := state.PageRequest{Size: 100}
			c := collection{name: "candidates", table: "candidates", idColumn: "id", where: "1=1", evaluation: e}
			for {
				result, err := page(
					ctx, tx, request, c, func(id int64) (int64, error) {
						return id, nil
					},
				)
				if err != nil {
					return struct{}{}, err
				}
				for _, id := range result.Items {
					if err := ctx.Err(); err != nil {
						return struct{}{}, err
					}
					candidate, err := getCandidate(ctx, tx, id, e)
					if err != nil {
						return struct{}{}, err
					}
					more, err := visit(candidate)
					if err != nil || !more {
						callbackErr = err
						return struct{}{}, nil
					}
				}
				if result.Next == nil {
					return struct{}{}, nil
				}
				request.Cursor = result.Next
			}
		},
	)
	if callbackErr != nil {
		return callbackErr
	}
	return err
}
