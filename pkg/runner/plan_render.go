package runner

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func printPlan(out io.Writer, plan *runtime.Plan, ascii bool) {
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
		printDeferredReads(out, plan.Steps)
		return
	}

	renderPlanTree(out, tree, "", 0, ascii)
	printDeferredReads(out, plan.Steps)

	var leaves []*runtime.PlanStep
	collectChangedLeaves(tree, "", &leaves)
	c := summarize(leaves)
	fmt.Fprintln(out)
	fmt.Fprintf(out,
		"Plan: %d to create, %d to update, %d to replace, %d to destroy, %d to rerun.\n",
		c.create, c.update, c.replace, c.destroy, c.rerun)
}

// printDeferredReads lists every step whose read was held back by a
// pending configuration, so a plan that checked no drift for a node
// says so instead of staying silent. Resources fall back to stored
// state for their decision; data sources read at apply.
func printDeferredReads(out io.Writer, steps []*runtime.PlanStep) {
	var deferred []*runtime.PlanStep
	for _, s := range steps {
		if s.DeferredRead != "" {
			deferred = append(deferred, s)
		}
	}
	if len(deferred) == 0 {
		return
	}
	slices.SortFunc(deferred, func(a, b *runtime.PlanStep) int {
		return cmp.Compare(a.Address, b.Address)
	})
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Deferred reads (%d):\n", len(deferred))
	for _, s := range deferred {
		reason := "drift unchecked this plan"
		if s.Kind == runtime.NodeData {
			reason = "read deferred to apply"
		}
		fmt.Fprintf(out, "  %s    @configuration: %s pending; %s\n",
			s.Address, s.DeferredRead, reason)
	}
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
		if s.Composite {
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

func renderPlanTree(out io.Writer, t *planTree, parent string, depth int, ascii bool) {
	children := append([]*runtime.PlanStep{}, t.children[parent]...)
	slices.SortFunc(children, func(a, b *runtime.PlanStep) int {
		return cmp.Compare(a.Address, b.Address)
	})

	symPad := strings.Repeat("  ", depth+1)
	fieldPad := strings.Repeat("  ", depth+3)

	for i := 0; i < len(children); {
		child := children[i]
		if child.Composite {
			if !anyChangeRecursive(t, child.Address) {
				i++
				continue
			}
			sym := decisionSymbol(boundaryDecisionRecursive(t, child.Address), ascii)
			fmt.Fprintf(out, "%s%s %s  (composite)\n",
				symPad, sym, relTo(child.Address, parent))
			renderStepInputs(out, fieldPad, child)
			renderPlanTree(out, t, child.Address, depth+1, ascii)
			i++
			continue
		}
		tmpl, key := runtime.SplitInstanceAddress(child.Address)
		if key != "" {
			n := renderForEachGroup(out, t, parent, children, i, tmpl, depth, ascii)
			i += n
			continue
		}
		if !isChange(child.Decision) {
			i++
			continue
		}
		fmt.Fprintf(out, "%s%s %s%s\n",
			symPad, decisionSymbol(child.Decision, ascii), relTo(child.Address, parent),
			destroyNote(child))
		renderStepInputs(out, fieldPad, child)
		i++
	}
}

// renderStepInputs writes one line per input field of step. A field that
// changed from the prior apply reads as `old -> new`; one still waiting on an
// upstream shows the source addresses in angle brackets; a field that forces a
// replacement is tagged so the reason for the replace is visible.
func renderStepInputs(out io.Writer, pad string, step *runtime.PlanStep) {
	if step.Configuration != "" && step.Decision != runtime.DecisionDestroy {
		fmt.Fprintf(out, "%s@configuration: %s\n", pad,
			formatPending(runtime.PendingValue{Refs: []string{step.Configuration}}))
	}
	for _, key := range sortedMapKeys(step.Inputs) {
		fmt.Fprintf(out, "%s%s: %s%s\n",
			pad, key, renderInputValue(step, key), replaceNote(step, key))
	}
}

// renderInputValue renders a field's planned value. When the step records the
// inputs a prior apply saw and this field's value changed, it reads as
// `old -> new`; an unchanged field, or a create with no prior, shows the new
// value alone.
func renderInputValue(step *runtime.PlanStep, field string) string {
	newVal := newInputValue(step, field)
	prior, ok := step.PriorInputs[field]
	if !ok || sameJSONValue(prior, step.Inputs[field]) {
		return newVal
	}
	priorVal := formatValue(prior)
	if slices.Contains(step.SensitiveInputs, field) {
		priorVal = sensitivePlaceholder
	}
	return priorVal + " -> " + newVal
}

// newInputValue renders the field's new value: a masked placeholder for a
// sensitive field, the upstream sources for one still waiting on an upstream,
// otherwise the formatted value.
func newInputValue(step *runtime.PlanStep, field string) string {
	if slices.Contains(step.SensitiveInputs, field) {
		return sensitivePlaceholder
	}
	v := step.Inputs[field]
	if v == nil {
		if refs := step.UnresolvedInputs[field]; len(refs) > 0 {
			return formatPending(runtime.PendingValue{Refs: refs})
		}
	}
	return formatValue(v)
}

// replaceNote tags a field the plan flagged as forcing a replacement.
func replaceNote(step *runtime.PlanStep, field string) string {
	if slices.Contains(step.ReplaceTriggers, field) {
		return "  (forces replacement)"
	}
	return ""
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
	ascii bool,
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
	header := decisionSymbol(strongestDecision(changing), ascii)
	fmt.Fprintf(out, "%s%s %s  (for-each, %d instances)\n",
		symPad, header, relTo(tmpl, parent), len(group))
	for _, inst := range changing {
		_, k := runtime.SplitInstanceAddress(inst.Address)
		fmt.Fprintf(out, "%s%s ['%s']%s\n",
			instSymPad, decisionSymbol(inst.Decision, ascii), k, destroyNote(inst))
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
		if child.Composite {
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
			if child.Composite {
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
		if child.Composite {
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

func printDriftStep(out io.Writer, s *runtime.PlanStep) {
	if s.Gone() {
		fmt.Fprintf(out, "  ! %s  (no longer present)\n", s.Address)
		return
	}
	fmt.Fprintf(out, "  ~ %s\n", s.Address)
	for _, key := range driftedFields(s) {
		prior := formatValue(s.PriorOutputs[key])
		observed := formatValue(s.ObservedOutputs[key])
		if slices.Contains(s.SensitiveOutputs, key) {
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
	slices.Sort(keys)
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

// formatValue renders v as a one-line UB literal: strings single-quoted, keys
// as bare idents where they can be, nested lists and maps inline. A
// PendingValue inside a list or map renders as its `<source>` placeholder so a
// value still settling at plan time stays readable; every other scalar routes
// through the canonical UB renderer, so the plan shows the same syntax the
// source is written in.
func formatValue(v any) string {
	switch x := v.(type) {
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
	case runtime.PendingValue:
		return formatPending(x)
	case nil:
		return "null"
	default:
		return lang.Render(v)
	}
}

// formatPending renders an unresolved value as the upstream sources it waits
// on, in angle brackets, so a list of pending elements reads as
// `[<a>, <b>]`. A pending value with no recorded source reads as <unknown>.
func formatPending(p runtime.PendingValue) string {
	if len(p.Refs) == 0 {
		return "<unknown>"
	}
	return "<" + strings.Join(p.Refs, ", ") + ">"
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
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

// Plan-decision glyphs for the default output: a clockwise arrow (replace),
// a counterclockwise arrow (rerun), a skip-forward bar (skip), and a
// leftward arrow (read). ascii mode swaps in plain forms.
var (
	glyphReplace = "↻"
	glyphRerun   = "↺"
	glyphSkip    = "⏭"
	glyphRead    = "←"
)

// decisionSymbol returns the marker for a decision. The default output uses
// one-glyph arrows; ascii mode uses padded word labels so the marks read
// clearly without a legend and the addresses still line up.
func decisionSymbol(d runtime.Decision, ascii bool) string {
	if ascii {
		return asciiLabel(d)
	}
	switch d {
	case runtime.DecisionCreate:
		return "+"
	case runtime.DecisionUpdate:
		return "~"
	case runtime.DecisionReplace:
		return glyphReplace
	case runtime.DecisionDestroy:
		return "-"
	case runtime.DecisionRerun:
		return glyphRerun
	case runtime.DecisionSkip:
		return glyphSkip
	case runtime.DecisionRead:
		return glyphRead
	case runtime.DecisionNoOp:
		return " "
	case runtime.DecisionEval:
		return "="
	}
	return "?"
}

// asciiLabel renders a decision as a parenthesized word, padded to the width
// of the longest label ("(replace)") so a column of marks stays aligned.
func asciiLabel(d runtime.Decision) string {
	word := "?"
	switch d {
	case runtime.DecisionCreate:
		word = "create"
	case runtime.DecisionUpdate:
		word = "update"
	case runtime.DecisionReplace:
		word = "replace"
	case runtime.DecisionDestroy:
		word = "destroy"
	case runtime.DecisionRerun:
		word = "rerun"
	case runtime.DecisionSkip:
		word = "skip"
	case runtime.DecisionRead:
		word = "read"
	case runtime.DecisionNoOp:
		word = "noop"
	case runtime.DecisionEval:
		word = "eval"
	}
	return fmt.Sprintf("%-9s", "("+word+")")
}
