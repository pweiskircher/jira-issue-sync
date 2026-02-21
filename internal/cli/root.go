package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pat/jira-issue-sync/internal/cli/middleware"
	"github.com/pat/jira-issue-sync/internal/commands"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/lock"
	"github.com/pat/jira-issue-sync/internal/output"
	"github.com/spf13/cobra"
)

type AppContext struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Now     func() time.Time
	WorkDir string
}

type GlobalFlags struct {
	JSON bool
}

type CommandContext struct {
	App         AppContext
	GlobalFlags *GlobalFlags
	CommandName contracts.CommandName
	DryRun      bool
}

func (ctx CommandContext) OutputMode() contracts.OutputMode {
	if ctx.GlobalFlags != nil && ctx.GlobalFlags.JSON {
		return contracts.OutputModeJSON
	}
	return contracts.OutputModeHuman
}

type executionState struct {
	global      GlobalFlags
	commandName string
	dryRun      bool
}

func (state *executionState) outputMode() contracts.OutputMode {
	if state.global.JSON {
		return contracts.OutputModeJSON
	}
	return contracts.OutputModeHuman
}

func (state *executionState) resolvedCommandName() string {
	if state.commandName != "" {
		return state.commandName
	}
	return "root"
}

type commandDefinition struct {
	Name           contracts.CommandName
	Short          string
	SupportsDryRun bool
}

var mvpCommandDefinitions = []commandDefinition{
	{Name: contracts.CommandInit, Short: "Initialize local issue sync workspace"},
	{Name: contracts.CommandPull, Short: "Pull Jira issues into local Markdown files"},
	{Name: contracts.CommandPush, Short: "Push local issue changes to Jira", SupportsDryRun: true},
	{Name: contracts.CommandSync, Short: "Push local changes then pull remote updates", SupportsDryRun: true},
	{Name: contracts.CommandStatus, Short: "Show local issue modification status"},
	{Name: contracts.CommandList, Short: "List local issues"},
	{Name: contracts.CommandNew, Short: "Create a new local issue draft"},
	{Name: contracts.CommandEdit, Short: "Open an issue in the configured editor"},
	{Name: contracts.CommandView, Short: "Render a local issue"},
	{Name: contracts.CommandDiff, Short: "Show local issue diff against last synced snapshot"},
}

// Run executes the CLI using shared output and exit-code plumbing.
func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	app := normalizeAppContext(AppContext{
		Stdout: stdout,
		Stderr: stderr,
		Now:    time.Now,
	})

	root, state := newRootCommand(app)
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		return int(contracts.ExitCodeSuccess)
	}

	var exitErr *codedExitError
	if errors.As(err, &exitErr) {
		return int(exitErr.Code)
	}

	report := output.Report{CommandName: state.resolvedCommandName(), DryRun: state.dryRun}
	if renderErr := output.Write(state.outputMode(), app.Stdout, app.Stderr, report, 0, err); renderErr != nil {
		_, _ = fmt.Fprintln(app.Stderr, output.FormatDiagnostic(renderErr))
	}

	return int(contracts.ExitCodeFatal)
}

// NewRootCommand constructs the Cobra command tree for the CLI.
func NewRootCommand(app AppContext) *cobra.Command {
	root, _ := newRootCommand(app)
	return root
}

func newRootCommand(app AppContext) (*cobra.Command, *executionState) {
	app = normalizeAppContext(app)
	state := &executionState{}
	lockPath := filepath.Join(app.WorkDir, contracts.DefaultLockFilePath)
	locker := lock.NewFileLock(lockPath, lock.Options{})

	root := &cobra.Command{
		Use:           "jira-issue-sync",
		Short:         "Sync Jira issues with local Markdown files",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVar(&state.global.JSON, "json", false, "emit machine-readable JSON envelope output")

	for _, def := range mvpCommandDefinitions {
		root.AddCommand(newStubCommand(app, state, def, locker))
	}

	return root, state
}

func newStubCommand(app AppContext, state *executionState, def commandDefinition, locker lock.Locker) *cobra.Command {
	dryRun := false
	stateFilter := "all"
	keyFilter := ""
	includeUnchanged := false
	pullProfile := ""
	pullJQL := ""

	cmd := &cobra.Command{
		Use:   string(def.Name),
		Short: def.Short,
		PreRun: func(cmd *cobra.Command, args []string) {
			state.commandName = string(def.Name)
			state.dryRun = dryRun
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			runner := middleware.WithCommandLock(def.Name, locker, func(ctx context.Context) error {
				start := app.Now()
				context := CommandContext{
					App:         app,
					GlobalFlags: &state.global,
					CommandName: def.Name,
					DryRun:      dryRun,
				}

				report, fatalErr, handled := runInspectionCommand(ctx, def.Name, app.WorkDir, stateFilter, keyFilter, includeUnchanged, pullProfile, pullJQL)
				if !handled {
					return runStub(context, app.Now().Sub(start))
				}

				report.CommandName = string(def.Name)
				report.DryRun = dryRun
				return renderAndResolveExit(context, report, app.Now().Sub(start), fatalErr)
			})
			return runner(cmd.Context())
		},
	}

	if def.SupportsDryRun {
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "simulate without applying remote writes")
	}

	if supportsInspectionFilters(def.Name) {
		cmd.Flags().StringVar(&stateFilter, "state", "all", "filter issues by local state (all|open|closed)")
		cmd.Flags().StringVar(&keyFilter, "key", "", "filter issues by key substring")
	}
	if supportsIncludeUnchanged(def.Name) {
		cmd.Flags().BoolVar(&includeUnchanged, "all", false, "include unchanged issues")
	}

	switch def.Name {
	case contracts.CommandPull:
		cmd.Flags().StringVar(&pullProfile, "profile", "", "profile name from config")
		cmd.Flags().StringVar(&pullJQL, "jql", "", "override JQL query")
	}

	return cmd
}

func supportsInspectionFilters(name contracts.CommandName) bool {
	switch name {
	case contracts.CommandList, contracts.CommandStatus, contracts.CommandDiff:
		return true
	default:
		return false
	}
}

func supportsIncludeUnchanged(name contracts.CommandName) bool {
	switch name {
	case contracts.CommandStatus, contracts.CommandDiff:
		return true
	default:
		return false
	}
}

func runInspectionCommand(ctx context.Context, commandName contracts.CommandName, workDir string, stateFilter string, keyFilter string, includeUnchanged bool, pullProfile string, pullJQL string) (output.Report, error, bool) {
	switch commandName {
	case contracts.CommandPull:
		report, err := commands.RunPull(ctx, workDir, commands.PullOptions{Profile: pullProfile, JQL: pullJQL})
		return report, err, true
	case contracts.CommandList:
		report, err := commands.RunList(workDir, commands.ListOptions{State: stateFilter, Key: keyFilter})
		return report, err, true
	case contracts.CommandStatus:
		report, err := commands.RunStatus(workDir, commands.StatusOptions{State: stateFilter, Key: keyFilter, IncludeUnchanged: includeUnchanged})
		return report, err, true
	case contracts.CommandDiff:
		report, err := commands.RunDiff(workDir, commands.DiffOptions{State: stateFilter, Key: keyFilter, IncludeUnchanged: includeUnchanged})
		return report, err, true
	default:
		return output.Report{}, nil, false
	}
}

func renderAndResolveExit(context CommandContext, report output.Report, duration time.Duration, fatalErr error) error {
	if err := output.Write(context.OutputMode(), context.App.Stdout, context.App.Stderr, report, duration, fatalErr); err != nil {
		return err
	}

	exitCode := output.ResolveExitCode(report, fatalErr)
	if exitCode == contracts.ExitCodeSuccess {
		return nil
	}

	return &codedExitError{Code: exitCode}
}

func normalizeAppContext(app AppContext) AppContext {
	if app.Now == nil {
		app.Now = time.Now
	}
	if app.WorkDir == "" {
		if wd, err := os.Getwd(); err == nil {
			app.WorkDir = wd
		} else {
			app.WorkDir = "."
		}
	}
	return app
}

func runStub(context CommandContext, duration time.Duration) error {
	report := output.Report{
		CommandName: string(context.CommandName),
		DryRun:      context.DryRun,
	}

	fatalErr := fmt.Errorf("command %q is not implemented yet", context.CommandName)
	if err := output.Write(context.OutputMode(), context.App.Stdout, context.App.Stderr, report, duration, fatalErr); err != nil {
		return err
	}

	return &codedExitError{Code: output.ResolveExitCode(report, fatalErr)}
}

type codedExitError struct {
	Code contracts.ExitCode
}

func (err codedExitError) Error() string {
	return fmt.Sprintf("exit with code %d", err.Code)
}
