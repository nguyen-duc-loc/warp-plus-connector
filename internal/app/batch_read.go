package app

import (
	"context"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// ShowCheck streams saved history without recovery or external operations.
func (a *App) ShowCheck(ctx context.Context, id int64, emit func(state.CheckRecord) error) (err error) {
	r, err := store.OpenReader(ctx, a.Settings.StateDir)
	if err != nil {
		return classified(err)
	}
	defer closeStore(r, &err)
	header, err := r.GetValidationReport(ctx, id)
	if err != nil {
		return classified(err)
	}
	output := func(record state.CheckRecord) error {
		if err := ctx.Err(); err != nil {
			return classified(err)
		}
		if err := emit(record); err != nil {
			return &Error{Code: "output_error"}
		}
		return nil
	}
	if err = output(state.CheckRecord{Header: &header}); err != nil {
		return err
	}
	p := state.PageRequest{Size: 100}
	if header.Policy == nil {
		for {
			page, err := r.ListAttempts(ctx, state.AttemptFilter{RunID: id}, p)
			if err != nil {
				return classified(err)
			}
			for _, attempt := range page.Items {
				detail := state.BatchAttempt{Attempt: attempt}
				if err = output(state.CheckRecord{Attempt: &detail}); err != nil {
					return err
				}
			}
			if page.Next == nil {
				return nil
			}
			p.Cursor = page.Next
		}
	}
	for {
		page, err := r.ListValidationItems(ctx, id, p)
		if err != nil {
			return classified(err)
		}
		for _, item := range page.Items {
			if err = output(state.CheckRecord{Item: &item}); err != nil {
				return err
			}
			attempts := state.PageRequest{Size: 100}
			for {
				history, err := r.ListBatchAttempts(ctx, id, item.CandidateID, attempts)
				if err != nil {
					return classified(err)
				}
				for _, detail := range history.Items {
					if err = output(state.CheckRecord{Attempt: &detail}); err != nil {
						return err
					}
				}
				if history.Next == nil {
					break
				}
				attempts.Cursor = history.Next
			}
		}
		if page.Next == nil {
			return nil
		}
		p.Cursor = page.Next
	}
}
