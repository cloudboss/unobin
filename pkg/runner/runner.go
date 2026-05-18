// Package runner is the CLI scaffolding every compiled stack binary
// links into. The generated `main.go` stays tiny: it embeds the
// stack-specific constants and calls Run with them.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/graphprint"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/spf13/cobra"
)

// EnvVarPrefix is the prefix unobin reads input overrides from. An env
// var like `UB_VAR_cluster_name=web-prod` overrides the `cluster-name`
// input, with snake case converted to kebab case.
const EnvVarPrefix = "UB_VAR_"

// Info bundles everything a generated stack binary passes into Run.
// StackBody is the embedded stack source the binary parses on each
// invocation. ModulePath is the binary's module-path identity (the
// same shape Go modules use); the operator's `config.ub` asserts the
// same value under `stack.module-path`. An empty ModulePath disables
// that identity check.
type Info struct {
	StackName    string
	StackVersion string
	StackCommit  string
	StackBody    string
	ModulePath   string
	Modules      map[string]*runtime.Module
}

// Run builds the cobra command tree and executes it. The process exits
// with status code 1 on error.
func Run(info Info) {
	root := newRootCmd(info)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(info Info) *cobra.Command {
	root := &cobra.Command{
		Use:          info.StackName,
		Short:        "Compiled unobin stack " + info.StackName,
		SilenceUsage: true,
	}
	root.AddCommand(newVersionCmd(info))
	root.AddCommand(newPlanCmd(info))
	root.AddCommand(newApplyCmd(info))
	root.AddCommand(newRefreshCmd(info))
	root.AddCommand(newValidateCmd(info))
	root.AddCommand(newOutputCmd(info))
	root.AddCommand(newSchemaCmd(info))
	root.AddCommand(newStateCmd(info))
	root.AddCommand(newPrintGraphCmd(info))
	return root
}

func newVersionCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print stack identity",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s (commit %s)\n",
				info.StackName, info.StackVersion, info.StackCommit)
		},
	}
}

func newPlanCmd(info Info) *cobra.Command {
	var (
		configPath           string
		outPath              string
		allowVersionMismatch bool
		parallelism          int
	)
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would do",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			if err := verifyStackEnvelope(info, config, configPath, allowVersionMismatch); err != nil {
				return err
			}
			return doPlan(cmd, info, config, configPath, outPath, parallelism)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-deployment configuration.")
	cmd.Flags().StringVarP(&outPath, "out", "o", "",
		"Write the plan to this file so apply can consume it.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Run even when the config does not pin this binary's version.")
	cmd.Flags().IntVar(&parallelism, "parallelism", 0,
		"Override the in-flight cap baked into the plan."+
			" Zero (the default) falls back to config.ub, then to the runtime default.")
	return cmd
}

func newApplyCmd(info Info) *cobra.Command {
	var parallelism int
	cmd := &cobra.Command{
		Use:   "apply <plan-file>",
		Short: "Run a previously computed plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doApplyPlan(cmd, info, args[0], parallelism)
		},
	}
	cmd.Flags().IntVar(&parallelism, "parallelism", 0,
		"Override the in-flight cap baked into the plan."+
			" Zero (the default) uses the value the plan was computed with.")
	return cmd
}

func doApplyPlan(
	cmd *cobra.Command, info Info, planPath string, parallelismOverride int,
) error {
	enc, err := loadEncrypter(info, nil, "")
	if err != nil {
		return err
	}
	sealed, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	encoded, err := enc.Decrypt(sealed)
	if err != nil {
		return fmt.Errorf("apply: decrypt plan: %w", err)
	}
	pf, err := runtime.DecodePlan(encoded)
	if err != nil {
		return err
	}
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	store, err := loadStore(info, nil, "", pf.DeploymentID, enc)
	if err != nil {
		return err
	}
	configurations, err := decodeConfigurationsFromPlan(pf.RawConfigurations, info.Modules)
	if err != nil {
		return err
	}
	ctx, drain, stop := applySignalContext(cmd.ErrOrStderr())
	defer stop()
	parallelism := pf.Parallelism
	if parallelismOverride > 0 {
		parallelism = parallelismOverride
	}
	events := make(chan runtime.ApplyEvent, len(pf.Steps)*3+16)
	rendererDone := make(chan struct{})
	go func() {
		defer close(rendererDone)
		consumeApplyEvents(events, cmd.ErrOrStderr())
	}()
	exec := &runtime.Executor{
		DAG:            runtime.BuildDAG(f, info.Modules),
		Modules:        info.Modules,
		Configurations: configurations,
		Store:          store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
		Parallelism: parallelism,
		Drain:       drain,
		Events:      events,
	}
	res, err := exec.ApplyPlan(ctx, pf)
	close(events)
	<-rendererDone
	if err != nil {
		return err
	}
	for _, k := range sortedMapKeys(res.Outputs) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", k, lang.RenderPretty(res.Outputs[k]))
	}
	return nil
}

// applyDrainGrace is the time SIGINT-initiated drain has to let
// in-flight CRUD calls finish before the apply context is canceled.
const applyDrainGrace = 60 * time.Second

// applySignalContext wires up SIGINT and SIGTERM handling for apply.
// The first SIGINT closes the returned drain channel so the scheduler
// stops dispatching and starts a grace timer; a second SIGINT or any
// SIGTERM cancels the context immediately. stop must be called by
// the caller to release the signal handler when apply returns.
func applySignalContext(stderr io.Writer) (context.Context, <-chan struct{}, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	drain := make(chan struct{})
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		drainStarted := false
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				switch sig {
				case syscall.SIGINT:
					if !drainStarted {
						drainStarted = true
						close(drain)
						fmt.Fprintln(stderr,
							"Interrupted; letting in-flight steps finish."+
								" Press Ctrl-C again or send SIGTERM to abort.")
						go func() {
							select {
							case <-time.After(applyDrainGrace):
								cancel()
							case <-ctx.Done():
							}
						}()
						continue
					}
					cancel()
					return
				case syscall.SIGTERM:
					cancel()
					return
				}
			}
		}
	}()
	stop := func() {
		signal.Stop(sigCh)
		cancel()
		<-done
	}
	return ctx, drain, stop
}

func newRefreshCmd(info Info) *cobra.Command {
	var (
		configPath           string
		allowVersionMismatch bool
	)
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Update state to match what each resource currently reports",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			if err := verifyStackEnvelope(info, config, configPath, allowVersionMismatch); err != nil {
				return err
			}
			return doRefresh(cmd, info, config, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-deployment configuration.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Run even when the config does not pin this binary's version.")
	return cmd
}

func doRefresh(cmd *cobra.Command, info Info, config *lang.File, configPath string) error {
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	inputs, err := buildInputs(config, configPath,
		topLevelObject(f, "inputs"), topLevelArray(f, "constraints"))
	if err != nil {
		return err
	}
	configurations, _, err := loadConfigurations(config, configPath, info.Modules)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(info, config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, deploymentID(configPath), enc)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:            runtime.BuildDAG(f, info.Modules),
		Modules:        info.Modules,
		Inputs:         inputs,
		Configurations: configurations,
		Store:          store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
	}
	res, err := exec.Refresh(context.Background())
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Refreshed %d, dropped %d.\n", res.Refreshed, res.Dropped)
	if res.WrittenRev != "" {
		fmt.Fprintf(out, "State rev: %s\n", res.WrittenRev)
	}
	return nil
}

func newValidateCmd(info Info) *cobra.Command {
	var (
		configPath           string
		allowVersionMismatch bool
	)
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check stack source and config without reading state or resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			if err := verifyStackEnvelope(info, config, configPath, allowVersionMismatch); err != nil {
				return err
			}
			return doValidate(cmd, info, config, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub to validate alongside the stack source.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Validate even when the config does not pin this binary's version.")
	return cmd
}

func doValidate(cmd *cobra.Command, info Info, config *lang.File, configPath string) error {
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	_, err = buildInputs(config, configPath,
		topLevelObject(f, "inputs"), topLevelArray(f, "constraints"))
	if err != nil {
		return err
	}
	if _, _, err := loadConfigurations(config, configPath, info.Modules); err != nil {
		return err
	}
	if _, err := runtime.BuildDAG(f, info.Modules).TopologicalOrder(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "OK")
	return nil
}

func newPrintGraphCmd(info Info) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "print-graph",
		Short: "Print the stack's dependency graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doPrintGraph(cmd, info, format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "plain",
		"Output format: 'plain' for an indented text listing,"+
			" 'dot' for Graphviz.")
	return cmd
}

func doPrintGraph(cmd *cobra.Command, info Info, format string) error {
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	dag := runtime.BuildDAG(f, info.Modules)
	out := cmd.OutOrStdout()
	switch format {
	case "plain":
		graphprint.Plain(out, dag)
	case "dot":
		graphprint.DOT(out, dag, info.StackName)
	default:
		return fmt.Errorf("unknown --format %q (want 'plain' or 'dot')", format)
	}
	return nil
}

func newOutputCmd(info Info) *cobra.Command {
	var (
		asJSON     bool
		configPath string
	)
	cmd := &cobra.Command{
		Use:   "output [name]",
		Short: "Print stack outputs from the current state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			return doOutput(cmd, info, config, configPath, args, asJSON)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub identifying the deployment.")
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"Emit outputs as JSON instead of plain text.")
	return cmd
}

// parsedFile parses the stack source baked into the binary at compile
// time. The "stack.ub" filename labels error positions; the original
// source filename is not preserved across compile, so this label is
// the convention regardless of what the file was called on disk.
func parsedFile(info Info) (*lang.File, error) {
	f, err := lang.ParseSource("stack.ub", []byte(info.StackBody))
	if err != nil {
		return nil, err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	if errs := runtime.CheckReferences(f, info.Modules); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return f, nil
}

// loadStore resolves a state backend from the `state:` block of a
// pre-parsed config. With a nil file, the resolver falls back to the
// local backend at `.unobin/state`. deploymentID is the per-deployment
// directory name (the basename of config.ub for plan/refresh, or the
// plan file's embedded value for apply). configPath is preserved only
// for error messages.
func loadStore(
	info Info,
	f *lang.File,
	configPath, deploymentID string,
	enc sdkencrypt.Encrypter,
) (state.Backend, error) {
	sc, err := parseStateConfig(f, configPath)
	if err != nil {
		return nil, err
	}
	return resolveBackend(info, sc.Backend, info.StackName, deploymentID, enc)
}

// deploymentID derives a deployment id from the config file path. The
// basename minus any extension is the id, so `prod.ub` becomes "prod"
// and `staging.ub` becomes "staging". A missing config path falls back
// to "default" to keep the tests and dev workflows that pass no config
// running.
func deploymentID(configPath string) string {
	if configPath == "" {
		return "default"
	}
	base := filepath.Base(configPath)
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

// loadEncrypter resolves the encrypter from the `encryption:` sub-block
// of a pre-parsed config. With a nil file, or no encryption block
// present, the resolver falls back to the env-key encrypter against
// `UB_STATE_KEY`, or the no-op pass-through if that env var is unset.
// configPath is preserved only for error messages.
func loadEncrypter(info Info, f *lang.File, configPath string) (sdkencrypt.Encrypter, error) {
	sc, err := parseStateConfig(f, configPath)
	if err != nil {
		return nil, err
	}
	return resolveEncrypter(info, sc.Encrypter)
}

func doPlan(
	cmd *cobra.Command, info Info, config *lang.File,
	configPath, outPath string, parallelismOverride int,
) error {
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	inputs, err := buildInputs(config, configPath,
		topLevelObject(f, "inputs"), topLevelArray(f, "constraints"))
	if err != nil {
		return err
	}
	configurations, rawConfigurations, err := loadConfigurations(config, configPath, info.Modules)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(info, config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, deploymentID(configPath), enc)
	if err != nil {
		return err
	}
	parallelism, err := loadParallelism(config, configPath)
	if err != nil {
		return err
	}
	if parallelismOverride > 0 {
		parallelism = parallelismOverride
	}
	exec := &runtime.Executor{
		DAG:            runtime.BuildDAG(f, info.Modules),
		Modules:        info.Modules,
		Inputs:         inputs,
		Configurations: configurations,
		Store:          store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
		Parallelism: parallelism,
	}
	plan, err := exec.Plan(context.Background())
	if err != nil {
		return err
	}
	plan.RawConfigurations = rawConfigurations
	printPlan(cmd.OutOrStdout(), plan)
	if outPath != "" {
		encoded, err := runtime.EncodePlan(plan)
		if err != nil {
			return err
		}
		sealed, err := enc.Encrypt(encoded)
		if err != nil {
			return err
		}
		if err := ufs.WriteFileAtomic(outPath, sealed, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func buildInputs(
	f *lang.File,
	configPath string,
	decl *lang.ObjectLit,
	constraints *lang.ArrayLit,
) (map[string]any, error) {
	inputs, err := loadConfigInputs(f, configPath)
	if err != nil {
		return nil, err
	}
	applyEnvOverrides(inputs)
	validated, errs := lang.ValidateInputs(decl, inputs, defaultEval)
	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	cerrs := lang.CheckConstraints(constraints, validated, predicateEval(validated))
	if cerrs.Len() > 0 {
		return nil, cerrs.Err()
	}
	return validated, nil
}

// defaultEval reduces an `optional(T, default)` default expression to a
// Go value. The empty EvalContext means defaults can use literals,
// arithmetic, and built-in calls but not address roots like var.X
// (which would be circular at default-application time anyway).
func defaultEval(e lang.Expr) (any, error) {
	return runtime.Eval(e, &runtime.EvalContext{})
}

// predicateEval reduces a constraint's `when:` or `require:` expression
// against the validated inputs, so a predicate can read var.X for any
// declared input.
func predicateEval(values map[string]any) lang.EvalFunc {
	ctx := &runtime.EvalContext{Vars: values}
	return func(e lang.Expr) (any, error) {
		return runtime.Eval(e, ctx)
	}
}

// loadParallelism extracts the `parallelism: N` top-level field from
// a pre-parsed config. Zero is returned when the file omits the field
// or when f is nil, signaling the runtime should pick its default.
// path is preserved only for error messages.
func loadParallelism(f *lang.File, path string) (int, error) {
	if f == nil {
		return 0, nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "parallelism" {
			continue
		}
		val, err := runtime.Eval(fld.Value, &runtime.EvalContext{})
		if err != nil {
			return 0, fmt.Errorf("config %s: parallelism: %w", path, err)
		}
		n, ok := val.(int64)
		if !ok {
			return 0, fmt.Errorf(
				"config %s: parallelism: want a positive integer, got %s",
				path, lang.TypeMessage(val))
		}
		if n < 1 {
			return 0, fmt.Errorf(
				"config %s: parallelism: want a positive integer, got %d",
				path, n)
		}
		return int(n), nil
	}
	return 0, nil
}

// loadConfigInputs extracts the `inputs:` block from a pre-parsed
// config. A nil file returns an empty map with no error. path is
// preserved only for error messages.
func loadConfigInputs(f *lang.File, path string) (map[string]any, error) {
	if f == nil {
		return map[string]any{}, nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "inputs" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return nil, fmt.Errorf("config %s: `inputs:` must be an object", path)
		}
		val, err := runtime.Eval(obj, &runtime.EvalContext{})
		if err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		out, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config %s: `inputs:` evaluated to %T, want map", path, val)
		}
		return out, nil
	}
	return map[string]any{}, nil
}

// applyEnvOverrides reads UB_VAR_<name> environment variables and writes
// them into inputs. Underscores in the env name become hyphens to match
// kebab case input names. Each value is parsed as a UB literal:
// `UB_VAR_size=5` arrives as int64, `UB_VAR_use_spot=true` as bool,
// `UB_VAR_subnets=['a', 'b']` as a list. Values that do not parse fall
// through to a plain string, so URLs, paths, and names with special
// characters work without shell-escaped quotes.
func applyEnvOverrides(inputs map[string]any) {
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, EnvVarPrefix) {
			continue
		}
		eq := strings.IndexByte(env, '=')
		if eq < 0 {
			continue
		}
		name := strings.ReplaceAll(env[len(EnvVarPrefix):eq], "_", "-")
		if name == "" {
			continue
		}
		inputs[name] = parseEnvValue(env[eq+1:])
	}
}

// parseEnvValue tries to read raw as a single UB literal expression. A
// successful parse and Eval returns the typed value; any failure
// (parse error, multi-expression, eval error) falls through to the
// raw string so values that look like URLs or paths still arrive as
// strings without shell-escape ceremony.
func parseEnvValue(raw string) any {
	src := []byte("v: " + raw + "\n")
	f, err := lang.ParseSource("env", src)
	if err != nil {
		return raw
	}
	if f.Body == nil || len(f.Body.Fields) != 1 {
		return raw
	}
	val, err := runtime.Eval(f.Body.Fields[0].Value, &runtime.EvalContext{})
	if err != nil {
		return raw
	}
	return val
}

func doOutput(
	cmd *cobra.Command, info Info, config *lang.File, configPath string,
	args []string, asJSON bool,
) error {
	enc, err := loadEncrypter(info, config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, deploymentID(configPath), enc)
	if err != nil {
		return err
	}
	snap, err := store.Current()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), snap.Outputs)
		}
		for _, k := range sortedMapKeys(snap.Outputs) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", k, lang.RenderPretty(snap.Outputs[k]))
		}
		return nil
	}
	name := args[0]
	val, ok := snap.Outputs[name]
	if !ok {
		return fmt.Errorf("no output %q", name)
	}
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), val)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", lang.RenderPretty(val))
	return nil
}

func writeJSON(out io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, _ = out.Write(b)
	_, _ = out.Write([]byte{'\n'})
	return nil
}
