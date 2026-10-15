package cli

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
)

// resolveAbsent resolves an absent configured path through its nearest existing
// ancestor, without creating it or treating an unreadable ancestor as absence.
func resolveAbsent(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, e := os.Lstat(path); e == nil {
		return "", err
	} // dangling symlink is not a missing component
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	base, err := resolveAbsent(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(path)), nil
}

func terminalDestination(ctx context.Context, path string, exclusions []string) (string, os.FileInfo, error) {
	invalid := func() (string, os.FileInfo, error) { return "", nil, &app.Error{Code: "export_destination_invalid"} }
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if path == "" || path == "-" || strings.HasSuffix(path, string(os.PathSeparator)) {
		return invalid()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return invalid()
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return invalid()
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return invalid()
	}
	resolved := make([]string, 0, len(exclusions))
	for _, path := range exclusions {
		p, e := resolveAbsent(path)
		if e != nil {
			return invalid()
		}
		resolved = append(resolved, p)
	}
	dest, err := excludedDestination(abs, parent, resolved)
	if err != nil {
		return "", nil, &app.Error{Code: deliveryFailure(err).code}
	}
	return dest, info, nil
}

func terminalExport(
	ctx context.Context, review terminal.ExportReview, rows terminal.ExportRows,
	observe func(string, terminal.ExportReceipt),
) (receipt terminal.ExportReceipt) {
	receipt = terminal.ExportReceipt{
		Destination: review.Destination, Selection: review.Selection, Format: review.Format,
		IncludeKeys: review.IncludeKeys, Count: review.Count, Published: new(false), Durability: "not_published",
	}
	fail := func(err error) {
		failure := deliveryFailure(err)
		receipt.ErrorCode, receipt.Remaining = failure.code, failure.remaining
	}
	currentParent, parentErr := os.Stat(filepath.Dir(review.Destination))
	if parentErr != nil || !os.SameFile(currentParent, review.ParentIdentity) {
		receipt.ErrorCode = "export_destination_invalid"
		return
	}
	dest, info, err := terminalDestination(ctx, review.Destination, review.Exclusions)
	if err != nil {
		fail(err)
		return
	}
	if dest != review.Destination || !os.SameFile(info, review.ParentIdentity) {
		receipt.ErrorCode = "export_destination_invalid"
		return
	}
	stage, err := stageDestination(dest)
	if err != nil {
		fail(err)
		return
	}
	defer func() {
		observe("cleanup", receipt)
		if e := stage.cleanup(); e != nil {
			if receipt.ErrorCode == "" {
				receipt.ErrorCode = "export_cleanup_failed"
			}
			receipt.Remaining = stage.directory
		}
		observe("cleanup", receipt)
	}()
	stage.observe = func(phase string, published *bool, durability string) {
		receipt.Published = published
		receipt.Durability = durability
		observe(phase, receipt)
	}
	writer := bufio.NewWriter(stage.file)
	encoder := candidateEncoder{
		writer: writer, format: review.Format, showKeys: review.IncludeKeys,
		selection: &exportSelection{Mode: review.Selection, MatchingCount: review.View.Count},
	}
	if err = encoder.start(); err != nil {
		receipt.ErrorCode = "export_prepare_failed"
		return
	}
	observe("encoding", receipt)
	err = rows(
		ctx, func(c state.Candidate) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			if e := encoder.item(c); e != nil {
				return e
			}
			receipt.Prepared++
			observe("encoding", receipt)
			return nil
		},
	)
	if err != nil {
		fail(err)
		return
	}
	if receipt.Prepared != review.Count {
		receipt.ErrorCode = "export_prepare_failed"
		return
	}
	result := app.CandidateResult{
		Evaluation: review.View.Evaluation, Query: review.View.Filters, ReturnedCount: receipt.Prepared,
	}
	if err = encoder.finish(result); err == nil {
		err = writer.Flush()
	}
	if err != nil {
		receipt.ErrorCode = "export_prepare_failed"
		return
	}
	if err = ctx.Err(); err != nil {
		fail(err)
		return
	}
	_, err = stage.publish(ctx)
	if err != nil {
		fail(err)
	}
	return
}
