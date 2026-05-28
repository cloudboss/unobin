package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

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
		parent := runtime.DirectParent(s.Address)
		if _, ok := t.boundaries[parent]; !ok {
			parent = ""
		}
		t.children[parent] = append(t.children[parent], s)
	}
	return t
}

func renderPlanTree(out io.Writer, t *planTree, parent string, depth int) {
	children := append([]*runtime.PlanStep{}, t.children[parent]...)
	sort.Slice(children, func(i, j int) bool { return children[i].Address < children[j].Address })

	symPad := strings.Repeat("  ", depth+1)
	fieldPad := strings.Repeat("  ", depth+3)

	for i := 0; i < len(children); {
		child := children[i]
		if child.Kind == runtime.NodeComposite {
			if !anyChangeRecursive(t, child.Address) {
				i++
				continue
			}
			sym := decisionSymbol(boundaryDecisionRecursive(t, child.Address))
			fmt.Fprintf(out, "%s%s %s  (library %s)\n",
				symPad, sym, child.Address, compositeRef(child.Address))
			renderStepInputs(out, fieldPad, child)
			renderPlanTree(out, t, child.Address, depth+1)
			i++
			continue
		}
		tmpl, key := runtime.SplitInstanceAddress(child.Address)
		if key != "" {
			n := renderForEachGroup(out, t, parent, children, i, tmpl, depth)
			i += n
			continue
		}
		if !isChange(child.Decision) {
			i++
			continue
		}
		fmt.Fprintf(out, "%s%s %s%s\n",
			symPad, decisionSymbol(child.Decision), relTo(child.Address, parent),
			destroyNote(child))
		renderStepInputs(out, fieldPad, child)
		i++
	}
}

// renderStepInputs writes one line per input field of step. Fields
// whose plan-time evaluation hit a forward reference render as
// `<source.address>` so operators see which upstream node will fill
// the value; the alternative was a misleading literal `null`.
func renderStepInputs(out io.Writer, pad string, step *runtime.PlanStep) {
	for _, key := range sortedMapKeys(step.Inputs) {
		fmt.Fprintf(out, "%s%s: %s\n", pad, key, renderInputValue(step, key))
	}
}

func renderInputValue(step *runtime.PlanStep, field string) string {
	if stringSetContains(step.SensitiveInputs, field) {
		return sensitivePlaceholder
	}
	if refs := step.UnresolvedInputs[field]; len(refs) > 0 {
		return "<" + strings.Join(refs, ", ") + ">"
	}
	return formatValue(step.Inputs[field])
}

// renderForEachGroup renders all per-instance steps that share the
// same template address as a single group: one header line carrying
// the template address and instance count, then each instance
// indented one level deeper. start is the first index in children
// belonging to the group; the returned count is how many entries
// were consumed.
func renderForEachGroup(
	out io.Writer,
	t *planTree,
	parent string,
	children []*runtime.PlanStep,
	start int,
	tmpl string,
	depth int,
) int {
	end := start
	for end < len(children) {
		t2, k2 := runtime.SplitInstanceAddress(children[end].Address)
		if t2 != tmpl || k2 == "" {
			break
		}
		end++
	}
	group := children[start:end]
	var changing []*runtime.PlanStep
	for _, g := range group {
		if isChange(g.Decision) {
			changing = append(changing, g)
		}
	}
	if len(changing) == 0 {
		return end - start
	}
	symPad := strings.Repeat("  ", depth+1)
	instSymPad := strings.Repeat("  ", depth+2)
	instFieldPad := strings.Repeat("  ", depth+4)
	header := decisionSymbol(strongestDecision(changing))
	fmt.Fprintf(out, "%s%s %s  (for-each, %d instances)\n",
		symPad, header, relTo(tmpl, parent), len(group))
	for _, inst := range changing {
		_, k := runtime.SplitInstanceAddress(inst.Address)
		fmt.Fprintf(out, "%s%s ['%s']%s\n",
			instSymPad, decisionSymbol(inst.Decision), k, destroyNote(inst))
		renderStepInputs(out, instFieldPad, inst)
	}
	return end - start
}

// strongestDecision picks the most consequential decision among a
// group of per-instance steps. Destroy > Replace > Create > Update >
// Rerun; anything else returns NoOp.
func strongestDecision(steps []*runtime.PlanStep) runtime.Decision {
	priority := map[runtime.Decision]int{
		runtime.DecisionDestroy: 5,
		runtime.DecisionReplace: 4,
		runtime.DecisionCreate:  3,
		runtime.DecisionUpdate:  2,
		runtime.DecisionRerun:   1,
	}
	best := runtime.DecisionNoOp
	bestPri := 0
	for _, s := range steps {
		if pri, ok := priority[s.Decision]; ok && pri > bestPri {
			bestPri = pri
			best = s.Decision
		}
	}
	return best
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
// call sites, e.g. "resource.A.B.C/resource.D.E.F" where the inner
// segment "resource.D.E.F" is the call's own category root plus
// "<alias>.<type>.<name>".
func compositeRef(address string) string {
	tail := address
	if i := strings.LastIndex(tail, "/"); i >= 0 {
		tail = tail[i+1:]
	}
	parts := strings.SplitN(tail, ".", 4)
	if len(parts) < 4 {
		return ""
	}
	return parts[1] + "." + parts[2]
}

func printDriftStep(out io.Writer, s *runtime.PlanStep) {
	if s.Gone() {
		fmt.Fprintf(out, "  ! %s  (no longer present)\n", s.Address)
		return
	}
	fmt.Fprintf(out, "  ~ %s\n", s.Address)
	for _, key := range driftedFields(s) {
		prior := formatValue(s.PriorOutputs[key])
		observed := formatValue(s.ObservedOutputs[key])
		if stringSetContains(s.SensitiveOutputs, key) {
			prior = sensitivePlaceholder
			observed = sensitivePlaceholder
		}
		fmt.Fprintf(out, "      %s: %s -> %s\n", key, prior, observed)
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
			parts = append(parts, fmt.Sprintf("%s: %s", lang.RenderKey(k), formatValue(x[k])))
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

// destroyNote annotates a destroy step the plan read as already
// absent, so the output shows there is no resource left to delete.
func destroyNote(s *runtime.PlanStep) string {
	if s.Decision == runtime.DecisionDestroy && s.AlreadyGone {
		return "  (already absent)"
	}
	return ""
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
