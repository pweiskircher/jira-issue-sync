package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	initProjectKey := ""
	initProfile := "default"
	initBaseURL := ""
	initEmail := ""
	initDefaultJQL := ""
	initProfileJQL := ""
	initForce := false

	newSummary := ""
	newIssueType := "Task"
	newStatus := "Open"
	newPriority := ""
	newAssignee := ""
	newLabels := ""
	newBody := ""

	editEditor := ""

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

				report, fatalErr, handled := runInspectionCommand(def.Name, app.WorkDir, stateFilter, keyFilter, includeUnchanged)
				if !handled {
					report, fatalErr, handled = runAuthoringCommand(ctx, def.Name, app.WorkDir, args, authoringRunOptions{
						initProjectKey: initProjectKey,
						initProfile:    initProfile,
						initBaseURL:    initBaseURL,
						initEmail:      initEmail,
						initDefaultJQL: initDefaultJQL,
						initProfileJQL: initProfileJQL,
						initForce:      initForce,
						newSummary:     newSummary,
						newIssueType:   newIssueType,
						newStatus:      newStatus,
						newPriority:    newPriority,
						newAssignee:    newAssignee,
						newLabels:      newLabels,
						newBody:        newBody,
						editEditor:     editEditor,
					})
				}
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
	case contracts.CommandInit:
		cmd.Flags().StringVar(&initProjectKey, "project-key", "", "project key for the default profile")
		cmd.Flags().StringVar(&initProfile, "profile", "default", "profile name to initialize")
		cmd.Flags().StringVar(&initBaseURL, "jira-base-url", "", "default Jira base URL")
		cmd.Flags().StringVar(&initEmail, "jira-email", "", "default Jira account email")
		cmd.Flags().StringVar(&initDefaultJQL, "default-jql", "", "global default JQL")
		cmd.Flags().StringVar(&initProfileJQL, "profile-jql", "", "profile-specific default JQL")
		cmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing config if present")
	case contracts.CommandNew:
		cmd.Flags().StringVar(&newSummary, "summary", "", "summary for the new local draft")
		cmd.Flags().StringVar(&newIssueType, "issue-type", "Task", "issue type for the new local draft")
		cmd.Flags().StringVar(&newStatus, "status", "Open", "initial local status")
		cmd.Flags().StringVar(&newPriority, "priority", "", "initial local priority")
		cmd.Flags().StringVar(&newAssignee, "assignee", "", "initial local assignee")
		cmd.Flags().StringVar(&newLabels, "labels", "", "comma-separated labels")
		cmd.Flags().StringVar(&newBody, "body", "", "optional markdown body for the draft")
	case contracts.CommandEdit:
		cmd.Flags().StringVar(&editEditor, "editor", "", "editor command (defaults to VISUAL/EDITOR)")
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

func runInspectionCommand(commandName contracts.CommandName, workDir string, stateFilter string, keyFilter string, includeUnchanged bool) (output.Report, error, bool) {
	switch commandName {
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

type authoringRunOptions struct {
	initProjectKey string
	initProfile    string
	initBaseURL    string
	initEmail      string
	initDefaultJQL string
	initProfileJQL string
	initForce      bool
	newSummary     string
	newIssueType   string
	newStatus      string
	newPriority    string
	newAssignee    string
	newLabels      string
	newBody        string
	editEditor     string
}

func runAuthoringCommand(ctx context.Context, commandName contracts.CommandName, workDir string, args []string, options authoringRunOptions) (output.Report, error, bool) {
	switch commandName {
	case contracts.CommandInit:
		report, err := commands.RunInit(workDir, commands.InitOptions{
			ProjectKey:  options.initProjectKey,
			Profile:     options.initProfile,
			JiraBaseURL: options.initBaseURL,
			JiraEmail:   options.initEmail,
			DefaultJQL:  options.initDefaultJQL,
			ProfileJQL:  options.initProfileJQL,
			Force:       options.initForce,
		})
		return report, err, true
	case contracts.CommandNew:
		report, err := commands.RunNew(workDir, commands.NewOptions{
			Summary:   options.newSummary,
			IssueType: options.newIssueType,
			Status:    options.newStatus,
			Priority:  options.newPriority,
			Assignee:  options.newAssignee,
			Labels:    parseLabels(options.newLabels),
			Body:      options.newBody,
		})
		return report, err, true
	case contracts.CommandEdit:
		if len(args) != 1 {
			return output.Report{}, fmt.Errorf("edit requires exactly one issue key argument"), true
		}
		report, err := commands.RunEdit(ctx, workDir, commands.EditOptions{Key: args[0], Editor: options.editEditor})
		return report, err, true
	case contracts.CommandView:
		if len(args) != 1 {
			return output.Report{}, fmt.Errorf("view requires exactly one issue key argument"), true
		}
		report, err := commands.RunView(workDir, commands.ViewOptions{Key: args[0]})
		return report, err, true
	default:
		return output.Report{}, nil, false
	}
}

func parseLabels(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			labels = append(labels, trimmed)
		}
	}
	return labels
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
