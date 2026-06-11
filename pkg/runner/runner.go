// Package runner is the CLI scaffolding every compiled factory binary
// links into. The generated `main.go` stays tiny: it embeds the
// factory-specific constants and calls Run with them.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudboss/unobin/pkg/check"
	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/graphprint"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/spf13/cobra"
)

// EnvVarPrefix is the prefix unobin reads input overrides from. An env
// var like `UB_VAR_cluster_name=web-prod` overrides the `cluster-name`
// input, with snake case converted to kebab case.
const EnvVarPrefix = "UB_VAR_"

// Info bundles everything a generated factory binary passes into Run.
// FactoryBody is the embedded factory source the binary parses on each
// invocation. LibraryPath is the binary's library-path identity (the
// same form Go libraries use); the operator's `config.ub` asserts the
// same value under `factory.library-path`. An empty LibraryPath disables
// that identity check.
type Info struct {
	FactoryName     string
	FactoryVersion  string
	ContentRevision string
	FactoryBody     string
	LibraryPath     string
	Libraries       map[string]*runtime.Library

	// UnobinVersion is the unobin version the factory was compiled
	// against, stamped at link time the way FactoryVersion is. Run
	// refuses to start when the binary links a different one; empty
	// (built outside the CLI) checks nothing.
	UnobinVersion string
}

// Run builds the cobra command tree and executes it. The process exits
// with status code 1 on error.
func Run(info Info) {
	if err := verifyLinkedUnobin(info.UnobinVersion); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := newRootCmd(info)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(info Info) *cobra.Command {
	root := &cobra.Command{
		Use:           info.FactoryName,
		Short:         "Compiled unobin factory " + info.FactoryName,
		SilenceUsage:  true,
		SilenceErrors: true,
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
	root.AddCommand(newPinCmd(info))
	return root
}

func newVersionCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print factory identity",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s (content-revision %s)\n",
				info.FactoryName, info.FactoryVersion, info.ContentRevision)
		},
	}
}

func newPlanCmd(info Info) *cobra.Command {
	var (
		configPath           string
		outPath              string
		allowVersionMismatch bool
		parallelism          int
		destroy              bool
		ascii                bool
	)
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would do",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			if err := verifyFactoryEnvelope(info, config, configPath, allowVersionMismatch); err != nil {
				return err
			}
			return doPlan(cmd, info, config, configPath, outPath, parallelism, destroy, ascii)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-stack configuration.")
	cmd.Flags().StringVarP(&outPath, "out", "o", "",
		"Write the plan to this file so apply can consume it.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Run even when the config does not pin this binary's version.")
	cmd.Flags().IntVar(&parallelism, "parallelism", 0,
		"Override the in-flight cap baked into the plan."+
			" Zero (the default) falls back to config.ub, then to the runtime default.")
	cmd.Flags().BoolVar(&destroy, "destroy", false,
		"Plan to destroy every resource in state instead of converging on the source.")
	cmd.Flags().BoolVar(&ascii, "ascii", false,
		"Render the plan with plain ASCII symbols instead of the default arrows.")
	return cmd
}

func newApplyCmd(info Info) *cobra.Command {
	var (
		parallelism int
		outputStr   string
	)
	cmd := &cobra.Command{
		Use:   "apply <plan-file>",
		Short: "Run a previously computed plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := ParseFormat(outputStr)
			if err != nil {
				return err
			}
			return doApplyPlan(cmd, info, args[0], parallelism, format)
		},
	}
	cmd.Flags().IntVar(&parallelism, "parallelism", 0,
		"Override the in-flight cap baked into the plan."+
			" Zero (the default) uses the value the plan was computed with.")
	cmd.Flags().StringVar(&outputStr, "output", "text",
		"Output format: text (human), json (NDJSON envelopes), unobin (one UB literal per line).")
	return cmd
}

func doApplyPlan(
	cmd *cobra.Command, info Info, planPath string, parallelismOverride int, format Format,
) error {
	sealed, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var enc sdkencrypt.Encrypter
	pf, err := runtime.OpenPlan(sealed, func(ref *runtime.StateRef) (sdkencrypt.Encrypter, error) {
		e, err := resolveEncrypter(fromRuntimeStateRef(ref))
		if err != nil {
			return nil, err
		}
		enc = e
		return e, nil
	})
	if err != nil {
		return err
	}
	f, dag, err := parsedFile(info)
	if err != nil {
		return err
	}
	store, err := resolveBackend(fromRuntimeStateRef(pf.Backend),
		info.FactoryName, pf.Stack, enc)
	if err != nil {
		return err
	}
	configurations, err := decodeConfigurationsFromPlan(pf.RawConfigurations, info.Libraries)
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
		consumeApplyEvents(events, cmd.ErrOrStderr(), format)
	}()
	exec := &runtime.Executor{
		Source:         f,
		DAG:            dag,
		Libraries:      info.Libraries,
		Configurations: configurations,
		Store:          store,
		Factory: state.FactoryInfo{
			Name:            info.FactoryName,
			Version:         info.FactoryVersion,
			ContentRevision: info.ContentRevision,
		},
		Parallelism: parallelism,
		Drain:       drain,
		Events:      events,
	}
	res, err := exec.ApplyPlan(ctx, pf)
	close(events)
	<-rendererDone
	if err != nil {
		if ae, ok := errors.AsType[*runtime.ApplyError](err); ok {
			renderApplyError(cmd.ErrOrStderr(), ae, format)
		}
		return err
	}
	return writeApplyOutputs(cmd.OutOrStdout(), format, res.Outputs, rootSensitiveOutputs(f))
}

// writeApplyOutputs prints the final outputs in the requested
// format. Text emits `name: value` lines; json and unobin emit one
// apply-output envelope per name in alphabetical order. Names in
// sensitive get their value masked with the placeholder.
func writeApplyOutputs(
	out io.Writer, format Format, outputs map[string]any, sensitive map[string]bool,
) error {
	if format != FormatJSON && format != FormatUnobin {
		for _, k := range sortedMapKeys(outputs) {
			value := lang.RenderPretty(outputs[k])
			if sensitive[k] {
				value = sensitivePlaceholder
			}
			fmt.Fprintf(out, "%s: %s\n", k, value)
		}
		return nil
	}
	for _, k := range sortedMapKeys(outputs) {
		env := applyOutputEnv{
			Kind:  "apply-output",
			Name:  k,
			Value: outputs[k],
		}
		if sensitive[k] {
			env.Value = sensitivePlaceholder
		}
		if err := writeEnvelope(out, format, env); err != nil {
			return err
		}
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
			if err := verifyFactoryEnvelope(info, config, configPath, allowVersionMismatch); err != nil {
				return err
			}
			return doRefresh(cmd, info, config, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-stack configuration.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Run even when the config does not pin this binary's version.")
	return cmd
}

func doRefresh(cmd *cobra.Command, info Info, config *lang.File, configPath string) error {
	f, dag, err := parsedFile(info)
	if err != nil {
		return err
	}
	inputs, err := buildInputs(config, configPath,
		topLevelObject(f, "inputs"), topLevelArray(f, "constraints"), info.Libraries)
	if err != nil {
		return err
	}
	configurations, _, err := loadConfigurations(config, configPath, info.Libraries,
		runtime.InternalConfigurationNames(f))
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, stackName(configPath), enc)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		Source:         f,
		DAG:            dag,
		Libraries:      info.Libraries,
		Inputs:         inputs,
		Configurations: configurations,
		Store:          store,
		Factory: state.FactoryInfo{
			Name:            info.FactoryName,
			Version:         info.FactoryVersion,
			ContentRevision: info.ContentRevision,
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
		Short: "Check factory source and config without reading state or resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			if err := verifyFactoryEnvelope(info, config, configPath, allowVersionMismatch); err != nil {
				return err
			}
			return doValidate(cmd, info, config, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub to validate alongside the factory source.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Validate even when the config does not pin this binary's version.")
	return cmd
}

func doValidate(cmd *cobra.Command, info Info, config *lang.File, configPath string) error {
	f, _, err := parsedFile(info)
	if err != nil {
		return err
	}
	// Validation is the one command whose job is to re-prove the
	// stack, so it runs the deep checks the other commands leave to
	// the compiler.
	checker := check.New(f, info.Libraries)
	if errs := checker.References(nil); errs.Len() > 0 {
		return errs.Err()
	}
	dag := checker.DAG()
	if _, err := buildInputs(config, configPath,
		topLevelObject(f, "inputs"), topLevelArray(f, "constraints"), info.Libraries); err != nil {
		return err
	}
	configurations, _, err := loadConfigurations(config, configPath, info.Libraries,
		runtime.InternalConfigurationNames(f))
	if err != nil {
		return err
	}
	if err := validateStateRefs(config, configPath); err != nil {
		return err
	}
	if _, err := dag.TopologicalOrder(); err != nil {
		return err
	}
	demand := &runtime.Executor{
		DAG:            dag,
		Libraries:      info.Libraries,
		Configurations: configurations,
	}
	if err := demand.CheckConfigurations(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "OK")
	return nil
}

// validateStateRefs looks up each named backend and encrypter against
// info.Libraries and decodes its body against the registered
// configuration schema. It stops short of constructing either object,
// so validate does no filesystem or network work. Unset refs are not
// checked; the resolver's defaults are always available.
func validateStateRefs(config *lang.File, configPath string) error {
	sc, err := parseStateConfig(config, configPath)
	if err != nil {
		return err
	}
	if sc.Backend != nil {
		bt, err := lookupBackendType(sc.Backend)
		if err != nil {
			return err
		}
		if _, err := decodeRefConfig(bt.Configuration, sc.Backend); err != nil {
			return fmt.Errorf("state: %w", err)
		}
	}
	if sc.Encrypter != nil {
		et, err := lookupEncrypterType(sc.Encrypter)
		if err != nil {
			return err
		}
		if _, err := decodeRefConfig(et.Configuration, sc.Encrypter); err != nil {
			return fmt.Errorf("encryption: %w", err)
		}
	}
	return nil
}

func newPrintGraphCmd(info Info) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "print-graph",
		Short: "Print the factory's dependency graph",
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
	_, dag, err := parsedFile(info)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	switch format {
	case "plain":
		graphprint.Plain(out, dag)
	case "dot":
		graphprint.DOT(out, dag, info.FactoryName)
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
		Short: "Print factory outputs from the current state",
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
		"Path to a config.ub identifying the stack.")
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"Emit outputs as JSON instead of plain text.")
	return cmd
}

// parsedFile parses the factory source baked into the binary at compile
// time and returns it with its dependency graph, built once here and
// shared by every command. The compiler proved the source's references
// and types before the binary existed, so the binary trusts them;
// validation re-checks only the schema shape the graph build assumes.
// The "main.ub" filename labels error positions; the original source
// filename is not preserved across compile, so this label is the
// convention regardless of what the file was called on disk.
func parsedFile(info Info) (*lang.File, *runtime.DAG, error) {
	f, err := lang.ParseSource("main.ub", []byte(info.FactoryBody))
	if err != nil {
		return nil, nil, err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, nil, errs.Err()
	}
	return f, runtime.BuildDAG(f, info.Libraries), nil
}

// loadStore resolves a state backend from the state: block of a
// pre-parsed config. A config without a state: block is an error; a
// backend must be configured explicitly. stack is the per-stack
// directory name (the basename of config.ub for plan/refresh, or the
// plan file's embedded value for apply). configPath is preserved only
// for error messages.
func loadStore(
	info Info,
	f *lang.File,
	configPath, stack string,
	enc sdkencrypt.Encrypter,
) (state.Backend, error) {
	sc, err := parseStateConfig(f, configPath)
	if err != nil {
		return nil, err
	}
	return resolveBackend(sc.Backend, info.FactoryName, stack, enc)
}

// stackName derives a stack name from the config file path. The
// basename minus any extension is the id, so `prod.ub` becomes "prod"
// and `staging.ub` becomes "staging". A missing config path falls back
// to "default" to keep the tests and dev workflows that pass no config
// running.
func stackName(configPath string) string {
	if configPath == "" {
		return "default"
	}
	base := filepath.Base(configPath)
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

// loadEncrypter resolves the encrypter from the `encryption:` block of
// a pre-parsed config. With a nil file, or no encryption block
// present, the resolver falls back to the env-key encrypter against
// `UB_STATE_KEY`, or the no-op pass-through if that env var is unset.
// configPath is preserved only for error messages.
func loadEncrypter(f *lang.File, configPath string) (sdkencrypt.Encrypter, error) {
	sc, err := parseStateConfig(f, configPath)
	if err != nil {
		return nil, err
	}
	return resolveEncrypter(sc.Encrypter)
}

func doPlan(
	cmd *cobra.Command, info Info, config *lang.File,
	configPath, outPath string, parallelismOverride int, destroy, ascii bool,
) error {
	f, dag, err := parsedFile(info)
	if err != nil {
		return err
	}
	inputs, err := buildInputs(config, configPath,
		topLevelObject(f, "inputs"), topLevelArray(f, "constraints"), info.Libraries)
	if err != nil {
		return err
	}
	configurations, rawConfigurations, err := loadConfigurations(
		config, configPath, info.Libraries, runtime.InternalConfigurationNames(f))
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, stackName(configPath), enc)
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
		Source:         f,
		DAG:            dag,
		Libraries:      info.Libraries,
		Inputs:         inputs,
		Configurations: configurations,
		Store:          store,
		Factory: state.FactoryInfo{
			Name:            info.FactoryName,
			Version:         info.FactoryVersion,
			ContentRevision: info.ContentRevision,
		},
		Parallelism: parallelism,
		Destroy:     destroy,
	}
	plan, err := exec.Plan(context.Background())
	if err != nil {
		return err
	}
	plan.RawConfigurations = rawConfigurations
	sc, err := parseStateConfig(config, configPath)
	if err != nil {
		return err
	}
	plan.Backend = toRuntimeStateRef(sc.Backend)
	printPlan(cmd.OutOrStdout(), plan, ascii)
	if outPath != "" {
		sealed, err := runtime.SealPlan(plan, toRuntimeStateRef(sc.Encrypter), enc)
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
	libs map[string]*runtime.Library,
) (map[string]any, error) {
	inputs, err := loadConfigInputs(f, configPath)
	if err != nil {
		return nil, err
	}
	applyEnvOverrides(inputs, decl)
	validated, errs := lang.ValidateInputs(decl, inputs, defaultEval)
	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	cerrs := lang.CheckConstraints(constraints, validated,
		predicateEval(validated, libs), lang.DisplayRooted)
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
// declared input, call functions from the factory's imported libraries,
// and read a field under an unset nested input as null.
func predicateEval(
	values map[string]any, libs map[string]*runtime.Library,
) lang.ConstraintEvalFunc {
	return func(e lang.Expr, binds []lang.EachBinding) (any, error) {
		ctx := &runtime.EvalContext{Vars: values, Libraries: libs, MissingAsNull: true}
		for _, b := range binds {
			if ctx.Each == nil {
				ctx.Each = map[string]lang.EachValue{}
			}
			ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
		}
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
		val, err := runtime.Eval(obj, runtime.NewEvalContext(f))
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
// kebab case input names. The declared input type directs the parse: a
// string input takes the raw text exactly as given, so a value that
// happens to look like another literal (true, 42) arrives unmangled,
// while every other type reads its value as a UB literal and, failing
// that, as JSON, so `UB_VAR_size=5` arrives as int64,
// `UB_VAR_subnets=['a', 'b']` as a list, and a value written as JSON --
// double-quoted, which UB does not accept -- as a map or list. A value
// that parses as neither falls through to the raw string and input
// validation reports it against the declaration.
func applyEnvOverrides(inputs map[string]any, decl *lang.ObjectLit) {
	declared := typecheck.InputsFromBlock(decl)
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
		if stringDeclared(declared, name) {
			inputs[name] = env[eq+1:]
			continue
		}
		inputs[name] = parseEnvValue(env[eq+1:])
	}
}

// stringDeclared reports whether the named input is declared as a
// string, an optional() wrapper included; its env value then needs no
// literal parse.
func stringDeclared(declared []typecheck.ObjectField, name string) bool {
	for _, f := range declared {
		if f.Name == name {
			return f.Type.Kind == typecheck.String
		}
	}
	return false
}

// parseEnvValue reads raw as a value for a non-string input. It tries a
// single UB literal first, then JSON. UB strings are single-quoted, so
// a value written as JSON -- double-quoted, the way another tool emits
// it -- does not parse as UB; the JSON pass accepts it as is. A value
// that parses as neither falls through to the raw string, so a URL or
// path arrives without shell-escape ceremony, and input validation
// reports it against the declaration.
func parseEnvValue(raw string) any {
	if v, ok := parseUBValue(raw); ok {
		return v
	}
	if v, ok := parseJSONValue(raw); ok {
		return v
	}
	return raw
}

// parseUBValue reads raw as a single UB literal expression, returning ok
// false when it does not parse to exactly one expression or fails to
// evaluate.
func parseUBValue(raw string) (any, bool) {
	f, err := lang.ParseSource("env", []byte("v: "+raw+"\n"))
	if err != nil || f.Body == nil || len(f.Body.Fields) != 1 {
		return nil, false
	}
	val, err := runtime.Eval(f.Body.Fields[0].Value, &runtime.EvalContext{})
	if err != nil {
		return nil, false
	}
	return val, true
}

// parseJSONValue reads raw as one JSON value. UB strings are
// single-quoted, so JSON's double-quoted form does not parse as UB;
// this accepts a value supplied as JSON. Trailing tokens make the parse
// fail, so "1 2" is not a value.
func parseJSONValue(raw string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return normalizeJSONNumbers(v), true
}

// normalizeJSONNumbers replaces every json.Number with the int64 or
// float64 a UB literal would produce -- an integral number is an int64,
// everything else a float64 -- and recurses through arrays and objects,
// so a decoded value matches its UB-literal equivalent.
func normalizeJSONNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i, e := range x {
			x[i] = normalizeJSONNumbers(e)
		}
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeJSONNumbers(e)
		}
		return x
	}
	return v
}

func doOutput(
	cmd *cobra.Command, info Info, config *lang.File, configPath string,
	args []string, asJSON bool,
) error {
	enc, err := loadEncrypter(config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, stackName(configPath), enc)
	if err != nil {
		return err
	}
	snap, err := store.Current()
	if err != nil {
		return err
	}
	source, _, err := parsedFile(info)
	if err != nil {
		return err
	}
	sensitive := rootSensitiveOutputs(source)
	masked := func(k string, v any) any {
		if sensitive[k] {
			return sensitivePlaceholder
		}
		return v
	}
	if len(args) == 0 {
		if asJSON {
			out := make(map[string]any, len(snap.Outputs))
			for k, v := range snap.Outputs {
				out[k] = masked(k, v)
			}
			return writeJSON(cmd.OutOrStdout(), out)
		}
		for _, k := range sortedMapKeys(snap.Outputs) {
			value := lang.RenderPretty(snap.Outputs[k])
			if sensitive[k] {
				value = sensitivePlaceholder
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", k, value)
		}
		return nil
	}
	name := args[0]
	val, ok := snap.Outputs[name]
	if !ok {
		return fmt.Errorf("no output %q", name)
	}
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), masked(name, val))
	}
	if sensitive[name] {
		fmt.Fprintln(cmd.OutOrStdout(), sensitivePlaceholder)
		return nil
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
