package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/spf13/cobra"
)

var errArguments = errors.New("invalid arguments, see command help")

func addRuntimeCommands(root *cobra.Command, version string) {
	crawl := &cobra.Command{
		Use: "crawl", Short: "Collect bounded pages from configured public channels", Args: noArguments,
	}
	commonFlags(crawl)
	var sourceNames []string
	crawl.Flags().StringArrayVar(&sourceNames, "source", nil, "Select a configured enabled channel (repeatable)")
	crawl.Flags().Int("page-limit", 1, "Override selected channel page limits (1 through 100)")
	crawl.RunE = func(cmd *cobra.Command, _ []string) error {
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		// Use the bound slice: GetStringArray serializes a single empty value
		// as an empty array, which would turn invalid selection into defaults.
		a.SourceNames = append([]string(nil), sourceNames...)
		if cmd.Flags().Changed("page-limit") {
			n, _ := cmd.Flags().GetInt("page-limit")
			a.PageLimit = &n
		}
		report, err := a.Crawl(cmd.Context())
		if report.Run.ID != 0 {
			err = errors.Join(err, renderCrawl(cmd.OutOrStdout(), report))
		}
		return err
	}
	show := &cobra.Command{Use: "show <run-id>", Short: "Read saved crawl coverage and progress", Args: oneID}
	commonFlags(show)
	show.RunE = func(cmd *cobra.Command, args []string) error {
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		id, _ := strconv.ParseInt(args[0], 10, 64)
		report, err := a.ShowCrawl(cmd.Context(), id)
		if err != nil {
			return err
		}
		return renderCrawl(cmd.OutOrStdout(), report)
	}
	crawl.AddCommand(show)
	check := &cobra.Command{
		Use: "check <candidate-id>...", Short: "Check selected saved candidates", Args: manyIDs,
	}
	commonFlags(check)
	validatorFlags(check)
	checkPolicyFlags(check)
	check.RunE = func(cmd *cobra.Command, args []string) error {
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		ids := make([]int64, len(args))
		for i, arg := range args {
			ids[i], _ = strconv.ParseInt(arg, 10, 64)
		}
		outputOK := true
		report, err := a.CheckBatch(
			cmd.Context(), ids, func(item state.ValidationItem) error {
				e := renderValidationItem(cmd.OutOrStdout(), item)
				if e != nil {
					outputOK = false
				}
				return e
			},
		)
		if report.Run.ID != 0 && outputOK {
			if e := renderValidationSummary(cmd.OutOrStdout(), report); e != nil {
				return &app.Error{Code: "output_error"}
			}
		}
		return err
	}
	checkShow := &cobra.Command{
		Use: "show <run-id>", Short: "Read saved check selection, policy, and attempts", Args: oneID,
	}
	commonFlags(checkShow)
	checkShow.RunE = func(cmd *cobra.Command, args []string) error {
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		id, _ := strconv.ParseInt(args[0], 10, 64)
		var report state.ValidationReport
		err = a.ShowCheck(
			cmd.Context(), id, func(record state.CheckRecord) error {
				if record.Header != nil {
					report = *record.Header
				}
				return renderCheckRecord(cmd.OutOrStdout(), record)
			},
		)
		if err != nil {
			return err
		}
		if err = renderValidationSummary(cmd.OutOrStdout(), report); err != nil {
			return &app.Error{Code: "output_error"}
		}
		return nil
	}
	check.AddCommand(checkShow)

	list := candidateCommand(false)
	export := candidateCommand(true)
	validation := &cobra.Command{Use: "validator", Short: "Manage isolated validation recovery"}
	recoverCommand := &cobra.Command{
		Use: "recover", Short: "Reconcile an earlier validation without a new registration", Args: noArguments,
	}
	commonFlags(recoverCommand)
	validatorFlags(recoverCommand)
	recoverCommand.RunE = func(cmd *cobra.Command, _ []string) error {
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		backend, ok := a.Validator.(interface{ Recover(context.Context) error })
		if !ok {
			return &validator.Error{Code: "validation_unsupported"}
		}
		if err := backend.Recover(cmd.Context()); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "cleanup=complete")
		return err
	}
	validation.AddCommand(recoverCommand)
	sources := &cobra.Command{
		Use: "sources", Short: "Show configured sources and effective page limits", Args: noArguments,
	}
	commonFlags(sources)
	sources.RunE = func(cmd *cobra.Command, _ []string) error {
		settings, err := resolveSettings(cmd)
		if err != nil {
			return err
		}
		b := strings.Builder{}
		if len(settings.Sources) == 0 {
			b.WriteString("No sources configured.\n")
		} else {
			b.WriteString("CHANNEL\tSOURCE\tENABLED\tPAGE_LIMIT\n")
			for _, s := range settings.Sources {
				fmt.Fprintf(&b, "%s\ttelegram:%s\t%t\t%d\n", s.Channel, s.Channel, s.Enabled, s.PageLimit)
			}
		}
		if _, err := io.WriteString(cmd.OutOrStdout(), b.String()); err != nil {
			return &app.Error{Code: "output_error"}
		}
		return nil
	}
	root.AddCommand(crawl, check, list, export, validation, sources)
	addScheduleCommands(root, version)
}

func validatorFlags(cmd *cobra.Command) {
	cmd.Flags().String("validator", "unsupported", "Validator backend: unsupported or docker")
	cmd.Flags().String("validator-image", "", "Immutable local Docker image ID from a release image or local build")
	cmd.Flags().String("validator-socket", "", "Absolute local Docker socket path (defaults to selected local context)")
}

func noArguments(_ *cobra.Command, args []string) error {
	if len(args) != 0 {
		return errArguments
	}
	return nil
}

func manyIDs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errArguments
	}
	for _, arg := range args {
		if err := oneID(cmd, []string{arg}); err != nil {
			return err
		}
	}
	return nil
}

func oneID(_ *cobra.Command, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return errArguments
	}
	for _, c := range args[0] {
		if c < '0' || c > '9' {
			return errArguments
		}
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return errArguments
	}
	return nil
}

func commonFlags(cmd *cobra.Command) {
	cmd.Flags().String("config", "", "Private JSON configuration file")
	cmd.Flags().String("state-dir", "", "State directory for this profile")
	cmd.Flags().String("recheck-interval", "6h", "Positive duration used to evaluate evidence freshness")
}

func resolveSettings(cmd *cobra.Command) (config.Settings, error) {
	boolFlag := func(name string) *bool {
		if !cmd.Flags().Changed(name) {
			return nil
		}
		v, _ := cmd.Flags().GetBool(name)
		return &v
	}
	flag := func(name string) *string {
		if !cmd.Flags().Changed(name) {
			return nil
		}
		value, _ := cmd.Flags().GetString(name)
		return &value
	}
	return config.Resolve(
		config.Overrides{
			CrawlEnabled: boolFlag("crawl-enabled"), ValidationEnabled: boolFlag("validation-enabled"),
			CrawlInterval: flag("crawl-interval"), FailureInterval: flag("failure-interval"),
			CheckWorkers: flag("workers"), CheckMaxAttempts: flag("max-attempts"),
			CheckMutationInterval: flag("mutation-interval"), CheckCandidateTimeout: flag("candidate-timeout"),
			ConfigPath: flag("config"), StateDir: flag("state-dir"), RecheckInterval: flag("recheck-interval"),
			Validator: flag("validator"), ValidatorImage: flag("validator-image"),
			ValidatorSocket: flag("validator-socket"),
		},
	)
}

func application(cmd *cobra.Command, version string) (*app.App, error) {
	settings, err := resolveSettings(cmd)
	if err != nil {
		return nil, err
	}
	var backend validator.Backend = validator.Unsupported{Version: version}
	if settings.Validator == "docker" && (cmd.Name() == "check" || cmd.Name() == "recover" || cmd.Name() == "run") {
		backend = &validator.Docker{
			Options: validator.DockerOptions{
				Image: settings.ValidatorImage, Socket: settings.ValidatorSocket, StateDir: settings.ValidatorStateDir,
			},
		}
	}
	return &app.App{
		Settings: settings, Source: telegram.New(version, time.Now, nil), Validator: backend, Clock: time.Now,
		Logger: invocation(cmd).logger,
	}, nil
}

func textValue(v *string) string {
	if v == nil {
		return "none"
	}
	return *v
}
func timeValue(v *time.Time) string {
	if v == nil {
		return "none"
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func idValue(v *int64) string {
	if v == nil {
		return "none"
	}
	return strconv.FormatInt(*v, 10)
}

func renderCrawl(w io.Writer, r state.CrawlReport) error {
	if len(r.Sources) > 0 {
		return renderPlan(w, r)
	}
	b := strings.Builder{}
	fmt.Fprintf(
		&b,
		"run_id=%d status=%s reason=%s counts_current=%t\n",
		r.Run.ID,
		r.Run.Status,
		textValue(r.Run.ReasonCode),
		r.Run.Status == state.Running,
	)
	if c := r.Coverage; c != nil {
		count := "none"
		if c.ParsedPostCount != nil {
			count = strconv.Itoa(*c.ParsedPostCount)
		}
		fmt.Fprintf(
			&b,
			"source=%s url=%s coverage=%s page_limit=%d "+
				"history_complete=false\nfetched_at=%s parsed_posts=%s "+
				"min_message_id=%s max_message_id=%s\n",
			c.SourceID,
			c.RequestedURL,
			c.CoverageMode,
			c.PageLimit,
			timeValue(c.FetchedAt),
			count,
			idValue(c.MinMessageID),
			idValue(c.MaxMessageID),
		)
	} else {
		b.WriteString("coverage unavailable\n")
	}
	fmt.Fprintf(&b, "saved_candidates=%d saved_sightings=%d\n", r.SavedCandidates, r.SavedSightings)
	_, err := io.WriteString(w, b.String())
	return err
}

func renderAttempt(w io.Writer, a state.Attempt) error {
	evidence := "none"
	if e := a.Evidence; e != nil {
		evidence = fmt.Sprintf(
			"kind=%s observed_at=%s account_type=%s rejection_code=%s",
			e.Kind,
			timeValue(&e.ObservedAt),
			e.AccountType,
			e.RejectionCode,
		)
	}
	_, err := fmt.Fprintf(
		w,
		"candidate_id=%d run_id=%d attempt_id=%d outcome=%s reason=%s backend=%s version=%s evidence=%s\n",
		a.CandidateID,
		a.RunID,
		a.ID,
		a.Outcome,
		textValue(a.ReasonCode),
		a.BackendID,
		a.BackendVersion,
		evidence,
	)
	return err
}

func renderCandidate(w io.Writer, c state.Candidate, showKeys bool) error {
	reason, definitive, evidenceAt := "none", "none", "none"
	if c.LatestAttempt != nil {
		reason = textValue(c.LatestAttempt.ReasonCode)
	}
	if a := c.LastDefinitiveAttempt; a != nil {
		definitive = string(a.Outcome)
		if a.Evidence != nil {
			evidenceAt = timeValue(&a.Evidence.ObservedAt)
		}
	}
	line := fmt.Sprintf(
		"%d\t%s\t%s\t%s\t%s\t%s\t%t",
		c.ID,
		c.LatestOutcome,
		reason,
		definitive,
		evidenceAt,
		c.Freshness,
		c.ClockAnomaly,
	)
	if showKeys {
		line += "\t" + strconv.Quote(c.Key)
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

// Diagnostic sanitizes parser and output errors before they reach stderr.
func Diagnostic(err error) string {
	var ui *terminal.Error
	if errors.As(err, &ui) {
		return ui.Code
	}
	var control *controlDeliveryError
	if errors.As(err, &control) {
		return control.Error()
	}
	var native *service.Error
	if errors.As(err, &native) {
		return native.Error()
	}
	var output *deliveryError
	if errors.As(err, &output) {
		return output.diagnostic()
	}
	var validation *validator.Error
	if errors.As(err, &validation) {
		return validation.Code
	}
	var runtime *app.Error
	if errors.As(err, &runtime) {
		return runtime.Code
	}
	for _, known := range []error{
		config.ErrInvalid, config.ErrAccess, config.ErrScheduleDisabled, config.ErrSelectionInvalid,
		config.ErrSelectionEmpty, errArguments,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	// Cobra includes supplied values in these errors. Preserve the category only.
	for _, prefix := range []string{
		"unknown command", "unknown flag", "unknown shorthand flag", "invalid argument", "flag needs an argument",
	} {
		if strings.HasPrefix(err.Error(), prefix) {
			return prefix + ", see command help"
		}
	}
	return "command or output failed"
}
