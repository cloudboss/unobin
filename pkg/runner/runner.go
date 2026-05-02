// Package runner is the CLI scaffolding every compiled stack binary
// links into. The generated `main.go` stays tiny: it embeds the
// stack-specific constants and calls Run with them.
package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

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
		Use:           info.StackName,
		Short:         "Compiled unobin stack " + info.StackName,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd(info))
	root.AddCommand(newPlanCmd(info))
	root.AddCommand(newApplyCmd(info))
	root.AddCommand(newOutputCmd(info))
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
	var configPath string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would do",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doPlan(cmd, info, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-deployment configuration.")
	return cmd
}

func newApplyCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Run the stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doApply(cmd, info, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to a config.ub for inputs and per-deployment configuration.")
	return cmd
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

func loadStore(info Info) (*state.LocalStore, error) {
	return state.NewLocalStore(".unobin/state", info.StackName, "default", state.NoopEncrypter{})
}

func doApply(cmd *cobra.Command, info Info, configPath string) error {
	inputs, err := buildInputs(configPath)
	if err != nil {
		return err
	}
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	store, err := loadStore(info)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f),
		Modules: info.Modules,
		Inputs:  inputs,
		Store:   store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
	}
	res, err := exec.Run(context.Background())
	if err != nil {
		return err
	}
	for k, v := range res.Outputs {
		fmt.Fprintf(cmd.OutOrStdout(), "%s = %v\n", k, v)
	}
	return nil
}

func doPlan(cmd *cobra.Command, info Info, configPath string) error {
	inputs, err := buildInputs(configPath)
	if err != nil {
		return err
	}
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	store, err := loadStore(info)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f),
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
	printPlan(cmd, plan)
	return nil
}

func printPlan(cmd *cobra.Command, plan *runtime.Plan) {
	out := cmd.OutOrStdout()
	if len(plan.Steps) == 0 {
		fmt.Fprintln(out, "No changes.")
		return
	}
	for _, step := range plan.Steps {
		fmt.Fprintf(out, "  %s %s\n", decisionSymbol(step.Decision), step.Address)
	}
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
	store, err := loadStore(info)
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
