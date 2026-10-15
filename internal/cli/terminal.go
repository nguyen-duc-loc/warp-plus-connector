package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/spf13/cobra"
)

func openTerminal(cmd *cobra.Command, release, commit string) error {
	if presentation(cmd).Validate() != nil {
		return errArguments
	}
	if !terminal.Admitted(cmd.InOrStdin(), cmd.OutOrStdout()) {
		return cmd.Help()
	}
	terminal.Start(cmd.Context())
	if cmd.Context().Err() != nil {
		return cmd.Context().Err()
	}
	settings, err := resolveSettings(cmd)
	if err != nil {
		return err
	}
	a := &app.App{
		Settings: settings, Clock: time.Now, OverviewService: func(ctx context.Context) state.OverviewService {
			account, err := service.ResolveAccount(ctx)
			if err != nil {
				return service.UnavailableOverview(err)
			}
			out := service.OverviewStatus(ctx, account)
			service.CompareOverviewCollection(settings.StateDir, &out)
			if ctx.Err() != nil {
				return service.UnavailableOverview(ctx.Err())
			}
			return out
		},
	}
	var suggestions [2]string
	for i, action := range []string{"sources", "crawl"} {
		suggestions[i] = "wpc " + action
		for _, name := range []string{"config", "state-dir", "recheck-interval"} {
			if cmd.Flags().Changed(name) {
				v, _ := cmd.Flags().GetString(name)
				suggestions[i] += " --" + name + " '" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'"
			}
		}
	}
	inv := invocation(cmd)
	if inv == nil {
		inv = newInvocation(cmd, presentation(cmd), nil)
		cmd.SetContext(context.WithValue(cmd.Context(), diagnosticKey{}, inv))
	}

	a.Source = telegram.New(release, time.Now, nil)
	a.Validator = validator.Unsupported{Version: release}
	if settings.Validator == "docker" {
		a.Validator = &validator.Docker{
			Options: validator.DockerOptions{
				Image: settings.ValidatorImage, Socket: settings.ValidatorSocket, StateDir: settings.ValidatorStateDir,
			},
		}
	}
	work := a.NewWorkSession()
	work.Guard = terminal.OperationGuard(cmd.Context())
	work.Observe = func(r app.WorkState) { inv.work.Store(&r) }
	return terminal.Run(
		cmd.Context(), cmd.InOrStdin().(*os.File), cmd.OutOrStdout().(*os.File), terminal.Options{
			Release: release, Commit: commit, Color: presentation(cmd).Color, StateDir: settings.StateDir,
			ConfigPath: settings.ConfigPath, Suggestions: suggestions, Read: a.ReadOverview,
			Results: a.NewResultsSession(),
			Work:    work, Settings: settings, Reports: a.NewReportSession(),
			Destination: terminalDestination, Export: terminalExport,
			ObserveExport: func(r terminal.ExportReceipt) { inv.export.Store(&r) },
		},
	)
}
