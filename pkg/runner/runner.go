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
		printGraphPlain(out, dag)
	case "dot":
		printGraphDot(out, dag, info.StackName)
	default:
		return fmt.Errorf("unknown --format %q (want 'plain' or 'dot')", format)
	}
	return nil
}

func printGraphPlain(out io.Writer, dag *runtime.DAG) {
	addrs := sortedNodeAddresses(dag)
	for i, a := range addrs {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, a)
		deps := append([]string{}, dag.Edges[a]...)
		sort.Strings(deps)
		for _, d := range deps {
			fmt.Fprintf(out, "  -> %s\n", d)
		}
	}
}

func printGraphDot(out io.Writer, dag *runtime.DAG, name string) {
	fmt.Fprintf(out, "digraph %q {\n", name)
	addrs := sortedNodeAddresses(dag)
	for _, a := range addrs {
		fmt.Fprintf(out, "  %q;\n", a)
	}
	for _, from := range addrs {
		deps := append([]string{}, dag.Edges[from]...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, ok := dag.Nodes[dep]; !ok {
				continue
			}
			fmt.Fprintf(out, "  %q -> %q;\n", from, dep)
		}
	}
	fmt.Fprintln(out, "}")
}

func sortedNodeAddresses(dag *runtime.DAG) []string {
	addrs := make([]string, 0, len(dag.Nodes))
	for a := range dag.Nodes {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	return addrs
}

func newOutputCmd(info Info) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "output [name]",
		Short: "Print stack outputs from the current state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doOutput(cmd, info, args, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"Emit outputs as JSON instead of plain text.")
	return cmd
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
	var drift []*runtime.PlanStep
	for _, s := range plan.Steps {
		if s.Drift() || s.Gone() {
			drift = append(drift, s)
		}
	}

	tree := buildPlanTree(plan.Steps)

	if len(drift) > 0 {
		fmt.Fprintf(out, "Drift detected (%d):\n", len(drift))
		for _, s := range drift {
			printDriftStep(out, s)
		}
		fmt.Fprintln(out)
	}

	if !anyChangeRecursive(tree, "") {
		fmt.Fprintln(out, "No changes.")
		return
	}

	renderPlanTree(out, tree, "", 0)

	var leaves []*runtime.PlanStep
	collectChangedLeaves(tree, "", &leaves)
	c := summarize(leaves)
	fmt.Fprintln(out)
	fmt.Fprintf(out,
		"Plan: %d to create, %d to update, %d to replace, %d to destroy, %d to rerun.\n",
		c.create, c.update, c.replace, c.destroy, c.rerun)
}

// planTree groups plan steps by their direct enclosing composite call
// site so the renderer can walk the composite hierarchy. children are
// indexed by parent address; the empty key holds steps whose direct
// parent is not a composite boundary in this plan (top-level steps and
// orphan destroys for removed call sites). boundaries holds every
// composite step keyed by address.
type planTree struct {
	children   map[string][]*runtime.PlanStep
	boundaries map[string]*runtime.PlanStep
}

func buildPlanTree(steps []*runtime.PlanStep) *planTree {
	t := &planTree{
		children:   map[string][]*runtime.PlanStep{},
		boundaries: map[string]*runtime.PlanStep{},
	}
	for _, s := range steps {
		if s.Kind == runtime.NodeComposite {
			t.boundaries[s.Address] = s
		}
	}
	for _, s := range steps {
		parent := directParent(s.Address)
		if _, ok := t.boundaries[parent]; !ok {
			parent = ""
		}
		t.children[parent] = append(t.children[parent], s)
	}
	return t
}

func directParent(addr string) string {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return ""
}

func renderPlanTree(out io.Writer, t *planTree, parent string, depth int) {
	children := append([]*runtime.PlanStep{}, t.children[parent]...)
	sort.Slice(children, func(i, j int) bool { return children[i].Address < children[j].Address })

	symPad := strings.Repeat("  ", depth+1)
	fieldPad := strings.Repeat("  ", depth+3)

	for _, child := range children {
		if child.Kind == runtime.NodeComposite {
			if !anyChangeRecursive(t, child.Address) {
				continue
			}
			sym := decisionSymbol(boundaryDecisionRecursive(t, child.Address))
			fmt.Fprintf(out, "%s%s %s  (module %s)\n",
				symPad, sym, child.Address, compositeRef(child.Address))
			for _, key := range sortedMapKeys(child.Inputs) {
				fmt.Fprintf(out, "%s%s: %s\n", fieldPad, key, formatValue(child.Inputs[key]))
			}
			renderPlanTree(out, t, child.Address, depth+1)
			continue
		}
		if !isChange(child.Decision) {
			continue
		}
		fmt.Fprintf(out, "%s%s %s\n",
			symPad, decisionSymbol(child.Decision), relTo(child.Address, parent))
		for _, key := range sortedMapKeys(child.Inputs) {
			fmt.Fprintf(out, "%s%s: %s\n", fieldPad, key, formatValue(child.Inputs[key]))
		}
	}
}

func anyChangeRecursive(t *planTree, parent string) bool {
	for _, child := range t.children[parent] {
		if child.Kind == runtime.NodeComposite {
			if anyChangeRecursive(t, child.Address) {
				return true
			}
			continue
		}
		if isChange(child.Decision) {
			return true
		}
	}
	return false
}

func boundaryDecisionRecursive(t *planTree, addr string) runtime.Decision {
	priority := map[runtime.Decision]int{
		runtime.DecisionDestroy: 5,
		runtime.DecisionReplace: 4,
		runtime.DecisionCreate:  3,
		runtime.DecisionUpdate:  2,
		runtime.DecisionRerun:   1,
	}
	best := runtime.DecisionNoOp
	bestPri := 0
	var visit func(p string)
	visit = func(p string) {
		for _, child := range t.children[p] {
			if child.Kind == runtime.NodeComposite {
				visit(child.Address)
				continue
			}
			if pri, ok := priority[child.Decision]; ok && pri > bestPri {
				bestPri = pri
				best = child.Decision
			}
		}
	}
	visit(addr)
	return best
}

func collectChangedLeaves(t *planTree, parent string, into *[]*runtime.PlanStep) {
	for _, child := range t.children[parent] {
		if child.Kind == runtime.NodeComposite {
			collectChangedLeaves(t, child.Address, into)
			continue
		}
		if isChange(child.Decision) {
			*into = append(*into, child)
		}
	}
}

// relTo returns addr with the parent prefix removed. Top-level steps
// (parent == "") are unchanged; composite-internal addresses lose
// only their direct enclosing prefix so a deeply-nested leaf reads
// as its single innermost segment under each boundary header.
func relTo(addr, parent string) string {
	if parent == "" {
		return addr
	}
	return strings.TrimPrefix(addr, parent+"/")
}

func isChange(d runtime.Decision) bool {
	switch d {
	case runtime.DecisionNoOp, runtime.DecisionSkip,
		runtime.DecisionRead, runtime.DecisionEval:
		return false
	}
	return true
}

// compositeRef extracts the trailing "<alias>.<composite-type>" pair
// from a boundary address. At root the address looks like
// "resource.greeter.greeting.welcome" and yields "greeter.greeting".
// For a nested boundary the prefix carries the chain of enclosing
// call sites, e.g. "resource.A.B.C/D.E.F" where the inner part
// "D.E.F" is the "<alias>.<type>.<name>" of the nested call.
func compositeRef(address string) string {
	tail := address
	if i := strings.LastIndex(tail, "/"); i >= 0 {
		tail = tail[i+1:]
	} else {
		tail = strings.TrimPrefix(tail, "resource.")
	}
	parts := strings.SplitN(tail, ".", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + "." + parts[1]
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

func doOutput(cmd *cobra.Command, info Info, args []string, asJSON bool) error {
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
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), snap.Outputs)
		}
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
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), val)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%v\n", val)
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
