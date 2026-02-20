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
				return runStub(context, app.Now().Sub(start))
			})
			return runner(cmd.Context())
		},
	}

	if def.SupportsDryRun {
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "simulate without applying remote writes")
	}

	return cmd
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
