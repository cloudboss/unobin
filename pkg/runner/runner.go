// Package runner is the CLI scaffolding every compiled stack binary
// links into. The generated `main.go` stays tiny: it embeds the
// stack-specific constants and calls Run with them.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state"
	"github.com/spf13/cobra"
)

// EnvVarPrefix is the prefix unobin reads input overrides from. An env
// var like `UB_VAR_cluster_name=web-prod` overrides the `cluster-name`
// input, with snake case converted to kebab case.
const EnvVarPrefix = "UB_VAR_"

// Info bundles everything a generated stack binary passes into Run.
type Info struct {
	StackName    string
	StackVersion string
	StackCommit  string
	StackSource  string
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
	)
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would do",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := verifyStackEnvelope(info, configPath, allowVersionMismatch)
			if err != nil {
				return err
			}
			return doPlan(cmd, info, configPath, outPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-deployment configuration.")
	cmd.Flags().StringVarP(&outPath, "out", "o", "",
		"Write the plan to this file so apply can consume it.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Run even when the config does not pin this binary's version.")
	return cmd
}

func newApplyCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "apply <plan-file>",
		Short: "Run a previously computed plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doApplyPlan(cmd, info, args[0])
		},
	}
}

func doApplyPlan(cmd *cobra.Command, info Info, planPath string) error {
	enc, err := loadEncrypter()
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
	store, err := loadStore(info, enc)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f, info.Modules),
		Modules: info.Modules,
		Store:   store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
	}
	res, err := exec.ApplyPlan(context.Background(), pf)
	if err != nil {
		return err
	}
	for k, v := range res.Outputs {
		fmt.Fprintf(cmd.OutOrStdout(), "%s = %v\n", k, v)
	}
	return nil
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
			err := verifyStackEnvelope(info, configPath, allowVersionMismatch)
			if err != nil {
				return err
			}
			return doRefresh(cmd, info, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-deployment configuration.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Run even when the config does not pin this binary's version.")
	return cmd
}

func doRefresh(cmd *cobra.Command, info Info, configPath string) error {
	inputs, err := buildInputs(configPath)
	if err != nil {
		return err
	}
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter()
	if err != nil {
		return err
	}
	store, err := loadStore(info, enc)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f, info.Modules),
		Modules: info.Modules,
		Inputs:  inputs,
		Store:   store,
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
			err := verifyStackEnvelope(info, configPath, allowVersionMismatch)
			if err != nil {
				return err
			}
			return doValidate(cmd, info, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub to validate alongside the stack source.")
	cmd.Flags().BoolVar(&allowVersionMismatch, "allow-version-mismatch", false,
		"Validate even when the config does not pin this binary's version.")
	return cmd
}

func doValidate(cmd *cobra.Command, info Info, configPath string) error {
	if _, err := buildInputs(configPath); err != nil {
		return err
	}
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	if _, err := runtime.BuildDAG(f, info.Modules).TopologicalOrder(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "OK")
	return nil
}

func newOutputCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "output [name]",
		Short: "Print stack outputs from the current state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doOutput(cmd, info, args)
		},
	}
}

func parsedFile(info Info) (*lang.File, error) {
	f, err := lang.ParseSource("stack.ub", []byte(info.StackSource))
	if err != nil {
		return nil, err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return f, nil
}

func loadStore(info Info, enc state.Encrypter) (*state.LocalStore, error) {
	return state.NewLocalStore(".unobin/state", info.StackName, "default", enc)
}

// loadEncrypter returns the Encrypter constructed from `UB_STATE_KEY`.
// When the environment variable is unset, it returns `NoopEncrypter` so
// development workflows and tests can run without a key configured.
func loadEncrypter() (state.Encrypter, error) {
	if os.Getenv("UB_STATE_KEY") == "" {
		return state.NoopEncrypter{}, nil
	}
	return state.NewEnvKeyEncrypter("UB_STATE_KEY")
}

func doPlan(cmd *cobra.Command, info Info, configPath, outPath string) error {
	inputs, err := buildInputs(configPath)
	if err != nil {
		return err
	}
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter()
	if err != nil {
		return err
	}
	store, err := loadStore(info, enc)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f, info.Modules),
		Modules: info.Modules,
		Inputs:  inputs,
		Store:   store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
	}
	plan, err := exec.Plan(context.Background())
	if err != nil {
		return err
	}
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

func printPlan(out io.Writer, plan *runtime.Plan) {
	var drift, changes []*runtime.PlanStep
	for _, s := range plan.Steps {
		if s.Drift() || s.Gone() {
			drift = append(drift, s)
		}
		switch s.Decision {
		case runtime.DecisionNoOp, runtime.DecisionSkip,
			runtime.DecisionRead, runtime.DecisionEval:
			continue
		}
		changes = append(changes, s)
	}
	if len(drift) > 0 {
		fmt.Fprintf(out, "Drift detected (%d):\n", len(drift))
		for _, s := range drift {
			printDriftStep(out, s)
		}
		fmt.Fprintln(out)
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, "No changes.")
		return
	}
	for _, step := range changes {
		fmt.Fprintf(out, "  %s %s\n", decisionSymbol(step.Decision), step.Address)
		for _, key := range sortedMapKeys(step.Inputs) {
			fmt.Fprintf(out, "      %s: %s\n", key, formatValue(step.Inputs[key]))
		}
	}
	c := summarize(changes)
	fmt.Fprintln(out)
	fmt.Fprintf(out,
		"Plan: %d to create, %d to update, %d to replace, %d to destroy, %d to rerun.\n",
		c.create, c.update, c.replace, c.destroy, c.rerun)
}

func printDriftStep(out io.Writer, s *runtime.PlanStep) {
	if s.Gone() {
		fmt.Fprintf(out, "  ! %s  (no longer present)\n", s.Address)
		return
	}
	fmt.Fprintf(out, "  ~ %s\n", s.Address)
	for _, key := range driftedFields(s) {
		fmt.Fprintf(out, "      %s: %s -> %s\n",
			key,
			formatValue(s.PriorOutputs[key]),
			formatValue(s.ObservedOutputs[key]))
	}
}

func driftedFields(s *runtime.PlanStep) []string {
	seen := map[string]bool{}
	for k := range s.PriorOutputs {
		seen[k] = true
	}
	for k := range s.ObservedOutputs {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		if !sameJSONValue(s.PriorOutputs[k], s.ObservedOutputs[k]) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func sameJSONValue(a, b any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

type planCounts struct {
	create, update, replace, destroy, rerun int
}

func summarize(steps []*runtime.PlanStep) planCounts {
	var c planCounts
	for _, s := range steps {
		switch s.Decision {
		case runtime.DecisionCreate:
			c.create++
		case runtime.DecisionUpdate:
			c.update++
		case runtime.DecisionReplace:
			c.replace++
		case runtime.DecisionDestroy:
			c.destroy++
		case runtime.DecisionRerun:
			c.rerun++
		}
	}
	return c
}

func formatValue(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case []any:
		parts := make([]string, len(x))
		for i, el := range x {
			parts[i] = formatValue(el)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedMapKeys(x)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", k, formatValue(x[k])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", x)
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func decisionSymbol(d runtime.Decision) string {
	switch d {
	case runtime.DecisionCreate:
		return "+"
	case runtime.DecisionUpdate:
		return "~"
	case runtime.DecisionReplace:
		return "R"
	case runtime.DecisionDestroy:
		return "-"
	case runtime.DecisionRerun:
		return ">"
	case runtime.DecisionSkip:
		return "."
	case runtime.DecisionNoOp:
		return " "
	case runtime.DecisionRead:
		return "?"
	case runtime.DecisionEval:
		return "="
	}
	return "?"
}

func buildInputs(configPath string) (map[string]any, error) {
	inputs := map[string]any{}
	if configPath != "" {
		loaded, err := loadConfigInputs(configPath)
		if err != nil {
			return nil, err
		}
		inputs = loaded
	}
	applyEnvOverrides(inputs)
	return inputs, nil
}

// loadConfigInputs reads a config .ub file and returns the evaluated
// inputs section. Other config sections are not consumed yet.
func loadConfigInputs(path string) (map[string]any, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := lang.ParseSource(path, src)
	if err != nil {
		return nil, err
	}
	f.Kind = lang.FileConfig
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
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
// kebab case input names. Values are taken as plain strings.
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
		inputs[name] = env[eq+1:]
	}
}

func doOutput(cmd *cobra.Command, info Info, args []string) error {
	enc, err := loadEncrypter()
	if err != nil {
		return err
	}
	store, err := loadStore(info, enc)
	if err != nil {
		return err
	}
	snap, err := store.Current()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		for k, v := range snap.Outputs {
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %v\n", k, v)
		}
		return nil
	}
	name := args[0]
	val, ok := snap.Outputs[name]
	if !ok {
		return fmt.Errorf("no output %q", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%v\n", val)
	return nil
}
