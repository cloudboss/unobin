package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/require"
)

func planTestExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
	store state.Backend,
	factory state.FactoryInfo,
) *Executor {
	t.Helper()
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{
		DAG: dag, SyntaxSource: syntaxSource, Libraries: libs, Store: store, Factory: factory,
	}
}

func planFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/plan", name)
}

// planThingConstraintErr plans a stack with one core.thing resource whose
// body is the given literal object, after attaching specs to the thing
// type's constraints, and returns the plan error (nil when it succeeds).
func planThingConstraintErr(t *testing.T, specs []lang.ConstraintSpec, body string) error {
	t.Helper()
	libs := resourceModules(&resourceCounters{})
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{"resource.thing": specs}
	src := fmt.Sprintf("%s: {\n  x: core.thing %s\n}\n", "resources", body)
	exec := planTestExecutor(t, src, libs, newStateStore(t),
		state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"})
	_, err := exec.Plan(context.Background())
	return err
}

func setSpec(kind string, fields ...string) lang.ConstraintSpec {
	return lang.ConstraintSpec{Kind: kind, Fields: fields}
}

func predSpec(when, require string) lang.ConstraintSpec {
	return lang.ConstraintSpec{Kind: "predicate", When: when, Require: require}
}

// goConstraintCases drives both the violation table and the determinism
// pass. The prefix repeated on every expected message is the node address
// plus the kind tag lang.Error renders.
const goConstraintPrefix = "resource.x: schema: "

func goConstraintCases() []struct {
	name    string
	specs   []lang.ConstraintSpec
	body    string
	wantErr string
} {
	return []struct {
		name    string
		specs   []lang.ConstraintSpec
		body    string
		wantErr string
	}{
		{
			name:  "exactly-one-of with two set is rejected",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + "constraints[0] (exactly-one-of [name, size]): " +
				"expected exactly one to be set, got 2 (name, size)",
		},
		{
			name:  "exactly-one-of with one set passes",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.name", "input.size")},
			body:  `{ name: 'a' }`,
		},
		{
			name:  "exactly-one-of with none set is rejected",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.name", "input.size")},
			body:  `{ region: 'us' }`,
			wantErr: goConstraintPrefix + "constraints[0] (exactly-one-of [name, size]): " +
				"expected exactly one to be set, got 0 ()",
		},
		{
			name:  "at-least-one-of with none set is rejected",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "input.name", "input.size")},
			body:  `{ region: 'us' }`,
			wantErr: goConstraintPrefix + "constraints[0] (at-least-one-of [name, size]): " +
				"expected at least one to be set, got none",
		},
		{
			name:  "at-least-one-of with both set passes",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "at-most-one-of with two set is rejected",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + "constraints[0] (at-most-one-of [name, size]): " +
				"expected at most one to be set, got 2 (name, size)",
		},
		{
			name:  "at-most-one-of with none set passes",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "input.name", "input.size")},
			body:  `{ region: 'us' }`,
		},
		{
			name:  "at-most-one-of with two set is rejected",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + "constraints[0] (at-most-one-of [name, size]): " +
				"expected at most one to be set, got 2 (name, size)",
		},
		{
			name:  "required-together with one set is rejected",
			specs: []lang.ConstraintSpec{setSpec("required-together", "input.name", "input.size")},
			body:  `{ name: 'a' }`,
			wantErr: goConstraintPrefix + "constraints[0] (required-together [name, size]): " +
				"expected all set or all null, got 1 set (name)",
		},
		{
			name:  "required-together with both set passes",
			specs: []lang.ConstraintSpec{setSpec("required-together", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "required-together with neither set passes",
			specs: []lang.ConstraintSpec{setSpec("required-together", "input.name", "input.size")},
			body:  `{ region: 'us' }`,
		},
		{
			name:  "required-with trigger lacking dependent is rejected",
			specs: []lang.ConstraintSpec{setSpec("required-with", "input.name", "input.size")},
			body:  `{ name: 'a' }`,
			wantErr: goConstraintPrefix + `constraints[0] (required-with): ` +
				`"name" is set, so [size] must also be set; missing size`,
		},
		{
			name:  "required-with trigger with dependent passes",
			specs: []lang.ConstraintSpec{setSpec("required-with", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "required-with without trigger passes",
			specs: []lang.ConstraintSpec{setSpec("required-with", "input.name", "input.size")},
			body:  `{ size: 1 }`,
		},
		{
			name:  "forbidden-with trigger and forbidden field is rejected",
			specs: []lang.ConstraintSpec{setSpec("forbidden-with", "input.name", "input.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + `constraints[0] (forbidden-with): ` +
				`"name" is set, so [size] must be null; got size`,
		},
		{
			name:  "forbidden-with trigger alone passes",
			specs: []lang.ConstraintSpec{setSpec("forbidden-with", "input.name", "input.size")},
			body:  `{ name: 'a' }`,
		},
		{
			name:    "predicate with unmet requirement is rejected",
			specs:   []lang.ConstraintSpec{predSpec("input.name != null", "input.size != null")},
			body:    `{ name: 'a' }`,
			wantErr: goConstraintPrefix + "constraints[0] (predicate): predicate requirement not satisfied",
		},
		{
			name:  "predicate with met requirement passes",
			specs: []lang.ConstraintSpec{predSpec("input.name != null", "input.size != null")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "predicate whose condition is false passes",
			specs: []lang.ConstraintSpec{predSpec("input.name != null", "input.size != null")},
			body:  `{ size: 1 }`,
		},
		{
			name: "predicate uses its custom message",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", When: "input.name != null",
				Require: "input.size != null", Message: "size is required with name",
			}},
			body:    `{ name: 'a' }`,
			wantErr: goConstraintPrefix + "constraints[0] (predicate): size is required with name",
		},
		{
			name: "two violated constraints report both",
			specs: []lang.ConstraintSpec{
				setSpec("exactly-one-of", "input.name", "input.size"),
				setSpec("forbidden-with", "input.name", "input.size"),
			},
			body: `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix +
				"constraints[0] (exactly-one-of [name, size]): " +
				"expected exactly one to be set, got 2 (name, size)\n" +
				goConstraintPrefix +
				`constraints[1] (forbidden-with): ` +
				`"name" is set, so [size] must be null; got size`,
		},
		{
			name:  "splat constraint names the violating element",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.items[*].a", "input.items[*].b")},
			body:  `{ items: [{ a: 1 }, { a: 1, b: 2 }] }`,
			wantErr: goConstraintPrefix +
				"constraints[0] (exactly-one-of [items[1].a, items[1].b]): " +
				"expected exactly one to be set, got 2 (items[1].a, items[1].b)",
		},
		{
			name:  "splat constraint passes when every element conforms",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.items[*].a", "input.items[*].b")},
			body:  `{ items: [{ a: 1 }, { b: 2 }] }`,
		},
	}
}

func TestPlanGoTypeConstraints(t *testing.T) {
	for _, tt := range goConstraintCases() {
		t.Run(tt.name, func(t *testing.T) {
			err := planThingConstraintErr(t, tt.specs, tt.body)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

// TestPlanGoTypeConstraintsDeterministic re-plans each case many times and
// requires the same error text, so map iteration order cannot reorder the
// reported violations.
func TestPlanGoTypeConstraintsDeterministic(t *testing.T) {
	for _, tt := range goConstraintCases() {
		t.Run(tt.name, func(t *testing.T) {
			first := planThingConstraintErr(t, tt.specs, tt.body)
			for range 10 {
				got := planThingConstraintErr(t, tt.specs, tt.body)
				if tt.wantErr == "" {
					require.NoError(t, got)
				} else {
					require.EqualError(t, got, first.Error())
				}
			}
		})
	}
}

// planForwardRefConstraintErr plans a stack of two nodes where thing b's
// body holds the given fields, at least one referencing the output of an
// upstream node of a constraint-free type, so b plans with an unresolved
// input. specs attach to the thing type alone, so any error is b's.
func planForwardRefConstraintErr(t *testing.T, specs []lang.ConstraintSpec, body string) error {
	t.Helper()
	c := &resourceCounters{}
	libs := resourceModules(c)
	libs["core"].Resources["plain"] = MakeResourceWith[countingResource, any, any](
		func() *countingResource { return &countingResource{counters: c} },
	)
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{"resource.thing": specs}
	src := fmt.Sprintf(
		"%s: {\n  a: core.plain { name: 'a' }\n  b: core.thing %s\n}\n",
		"resources",
		body,
	)
	exec := planTestExecutor(t, src, libs, newStateStore(t),
		state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"})
	_, err := exec.Plan(context.Background())
	return err
}

const forwardConstraintPrefix = "resource.b: schema: "

// forwardRefConstraintCases drives the per-rule deferral table and its
// determinism pass. In every deferred case the rule would report a
// violation if it ran against the pending field's null placeholder, so
// a missing error proves the rule waited; in every reported case the
// violated rule reads only resolved fields beside the pending one. A
// rule reading a pending field defers even when its known fields alone
// already violate it (the at-most-one-of and forbidden-with cases): a
// rule is judged only on complete values.
func forwardRefConstraintCases() []struct {
	name    string
	specs   []lang.ConstraintSpec
	body    string
	wantErr string
} {
	ref := "resource.a.id"
	return []struct {
		name    string
		specs   []lang.ConstraintSpec
		body    string
		wantErr string
	}{
		{
			name:  "exactly-one-of defers on a pending field",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.name", "input.size")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name:  "at-least-one-of defers on a pending field",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "input.name", "input.zone")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name: "at-most-one-of defers although known fields violate",
			specs: []lang.ConstraintSpec{
				setSpec("at-most-one-of", "input.name", "input.region", "input.zone"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
		},
		{
			name: "at-most-one-of defers although known fields violate",
			specs: []lang.ConstraintSpec{
				setSpec("at-most-one-of", "input.name", "input.region", "input.zone"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
		},
		{
			name:  "required-together defers on a pending field",
			specs: []lang.ConstraintSpec{setSpec("required-together", "input.name", "input.region")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
		},
		{
			name:  "required-with defers on a pending dependent",
			specs: []lang.ConstraintSpec{setSpec("required-with", "input.region", "input.name")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
		},
		{
			name: "forbidden-with defers although a known field violates",
			specs: []lang.ConstraintSpec{
				setSpec("forbidden-with", "input.region", "input.name", "input.zone"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
		},
		{
			name:  "predicate defers when require reads the pending field",
			specs: []lang.ConstraintSpec{predSpec("true", "input.name != null")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name:  "predicate defers when when reads the pending field",
			specs: []lang.ConstraintSpec{predSpec("input.name == null", "input.region != null")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name: "iterating predicate defers when require reads the pending field",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", ForEach: "input.regions",
				When: "true", Require: "input.name != null",
			}},
			body: `{ name: ` + ref + `, regions: ['us', 'eu'] }`,
		},
		{
			name:  "exactly-one-of reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "input.region", "input.zone")},
			body:  `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (exactly-one-of [region, zone]): " +
				"expected exactly one to be set, got 2 (region, zone)",
		},
		{
			name:  "at-least-one-of reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "input.region", "input.zone")},
			body:  `{ name: ` + ref + ` }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (at-least-one-of [region, zone]): " +
				"expected at least one to be set, got none",
		},
		{
			name:  "at-most-one-of reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "input.region", "input.zone")},
			body:  `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (at-most-one-of [region, zone]): " +
				"expected at most one to be set, got 2 (region, zone)",
		},
		{
			name:  "required-together reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("required-together", "input.region", "input.zone")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (required-together [region, zone]): " +
				"expected all set or all null, got 1 set (region)",
		},
		{
			name:  "required-with reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("required-with", "input.region", "input.zone")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
			wantErr: forwardConstraintPrefix +
				`constraints[0] (required-with): ` +
				`"region" is set, so [zone] must also be set; missing zone`,
		},
		{
			name:  "forbidden-with reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("forbidden-with", "input.region", "input.zone")},
			body:  `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				`constraints[0] (forbidden-with): ` +
				`"region" is set, so [zone] must be null; got zone`,
		},
		{
			name:  "predicate reports beside a pending field",
			specs: []lang.ConstraintSpec{predSpec("input.region != null", "input.zone != null")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (predicate): predicate requirement not satisfied",
		},
		{
			name: "splat rule reports when its list resolved",
			specs: []lang.ConstraintSpec{
				setSpec("exactly-one-of", "input.items[*].a", "input.items[*].b"),
			},
			body: `{ name: ` + ref + `, items: [{ a: 1, b: 2 }] }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (exactly-one-of [items[0].a, items[0].b]): " +
				"expected exactly one to be set, got 2 (items[0].a, items[0].b)",
		},
		{
			name: "indexed rule reports when its list resolved",
			specs: []lang.ConstraintSpec{
				setSpec("required-together", "input.items[0].a", "input.items[0].b"),
			},
			body: `{ name: ` + ref + `, items: [{ a: 1 }] }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (required-together [items[0].a, items[0].b]): " +
				"expected all set or all null, got 1 set (items[0].a)",
		},
		{
			name: "one rule reports while the others defer",
			specs: []lang.ConstraintSpec{
				setSpec("exactly-one-of", "input.region", "input.zone"),
				setSpec("exactly-one-of", "input.name", "input.size"),
				predSpec("true", "input.name != null"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (exactly-one-of [region, zone]): " +
				"expected exactly one to be set, got 2 (region, zone)",
		},
	}
}

// TestPlanChecksChainedForEachSpec proves a chained spec checks at the
// plan site through the real evaluator: the inner element is judged
// against its outer one, and the failure names the element through
// both levels.
func TestPlanChecksChainedForEachSpec(t *testing.T) {
	specs := []lang.ConstraintSpec{{
		Kind:    "predicate",
		When:    "true",
		Require: "@t.value.weight <= @rule.value.max-weight",
		Message: "a target cannot outweigh its rule",
		ForEachLevels: []lang.ForEachSpecLevel{
			{Name: "@rule", In: "input.rules"},
			{Name: "@t", In: "@rule.value.targets"},
		},
	}}
	err := planThingConstraintErr(t, specs, `{
    rules: [
      { max-weight: 10, targets: [{ weight: 5 }] },
      { max-weight: 10, targets: [{ weight: 5 }, { weight: 11 }] },
    ]
  }`)
	require.EqualError(t, err,
		"resource.x: schema: constraints[0] (predicate): "+
			"a target cannot outweigh its rule (rules[1].targets[1])")

	ok := planThingConstraintErr(t, specs, `{
    rules: [{ max-weight: 10, targets: [{ weight: 10 }] }]
  }`)
	require.NoError(t, ok)
}

// TestPlanGoTypeConstraintForwardRef proves the deferral is per rule,
// not per node: a node with a pending input still has every rule over
// resolved fields checked, and no rule reading the pending field runs.
func TestPlanGoTypeConstraintForwardRef(t *testing.T) {
	for _, tt := range forwardRefConstraintCases() {
		t.Run(tt.name, func(t *testing.T) {
			err := planForwardRefConstraintErr(t, tt.specs, tt.body)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

// TestPlanGoTypeConstraintForwardRefDeterministic re-plans each case
// many times and requires the same outcome, so map iteration over the
// unresolved set cannot flip a rule between deferred and checked.
func TestPlanGoTypeConstraintForwardRefDeterministic(t *testing.T) {
	for _, tt := range forwardRefConstraintCases() {
		t.Run(tt.name, func(t *testing.T) {
			first := planForwardRefConstraintErr(t, tt.specs, tt.body)
			for range 10 {
				got := planForwardRefConstraintErr(t, tt.specs, tt.body)
				if tt.wantErr == "" {
					require.NoError(t, got)
				} else {
					require.EqualError(t, got, first.Error())
				}
			}
		})
	}
}

// TestPlanGoTypeConstraintChecksActions confirms the check routes by node
// kind: the same constraint on an action type reports against the action
// address, not just resources.
func TestPlanGoTypeConstraintChecksActions(t *testing.T) {
	libs := resourceModules(&resourceCounters{})
	libs["core"].Actions = map[string]ActionRegistration{"echo": MakeAction[echoAction, any, any]()}
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{
		"action.echo": {{Kind: "exactly-one-of", Fields: []string{"input.name", "input.size"}}},
	}
	src := planFixture(t, "plan-go-type-constraint-checks-actions")
	exec := planTestExecutor(t, src, libs, newStateStore(t),
		state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"})
	_, err := exec.Plan(context.Background())
	require.EqualError(t, err,
		"action.x: schema: constraints[0] (exactly-one-of [name, size]): "+
			"expected exactly one to be set, got 2 (name, size)")
}

func runPlan(
	t *testing.T, src string, libraries map[string]*Library, store *local.Store,
) *Plan {
	t.Helper()
	exec := planTestExecutor(t, src, libraries, store,
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	return plan
}

func decisionFor(plan *Plan, addr string) Decision {
	if s := stepFor(plan, addr); s != nil {
		return s.Decision
	}
	return ""
}

func stepFor(plan *Plan, addr string) *PlanStep {
	for _, s := range plan.Steps {
		if s.Address == addr {
			return s
		}
	}
	return nil
}

// planCompositeConstraintErr plans a stack with a single composite whose
// body declares the given `inputs:` and `constraints:` blocks,
// instantiated with the given call-site args, and returns the plan error
// (nil when the plan succeeds). The composite's one internal resource
// uses literals so internal planning never depends on an unset input.
func planCompositeConstraintErr(t *testing.T, inputs, constraints, callArgs string) error {
	t.Helper()
	composite := syntaxResourceComposite(t, "pair", fmt.Sprintf(
		"%s %s\n%s %s\n%s: { one: core.thing { name: 'fixed', size: 1 } }\n",
		"inputs:",
		inputs,
		"constraints:",
		constraints,
		"resources",
	))
	libs := resourceModules(&resourceCounters{})
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"pair": composite,
		},
	}
	src := fmt.Sprintf("%s: {\n  x: w.pair %s\n}\n", "resources", callArgs)
	exec := planTestExecutor(t, src, libs, newStateStore(t),
		state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"})
	_, err := exec.Plan(context.Background())
	return err
}

func TestPlanCompositeConstraints(t *testing.T) {
	const inputs = `{
  name: { type: optional(string) }
  size: { type: optional(integer) }
}`
	const oneOf = `[ { kind: exactly-one-of, fields: [input.name, input.size] } ]`
	const together = `[ { kind: required-together, fields: [input.name, input.size] } ]`
	const predicate = `[ { kind: predicate, when: input.name != null, require: input.size != null } ]`
	tests := []struct {
		name        string
		constraints string
		callArgs    string
		wantErr     string
	}{
		{
			name:        "exactly-one-of with both set is rejected",
			constraints: oneOf,
			callArgs:    `{ name: 'a', size: 1 }`,
			wantErr: "resource.x: schema: constraints[0] (exactly-one-of " +
				"[name, size]): expected exactly one to be set, got 2 (name, size)",
		},
		{
			name:        "exactly-one-of with one set passes",
			constraints: oneOf,
			callArgs:    `{ name: 'a' }`,
		},
		{
			name:        "predicate with unmet requirement is rejected",
			constraints: predicate,
			callArgs:    `{ name: 'a' }`,
			wantErr: "resource.x: schema: constraints[0] (predicate): " +
				"predicate requirement not satisfied",
		},
		{
			name:        "predicate with met requirement passes",
			constraints: predicate,
			callArgs:    `{ name: 'a', size: 1 }`,
		},
		{
			name:        "predicate whose condition is false passes",
			constraints: predicate,
			callArgs:    `{ size: 1 }`,
		},
		{
			name:        "required-together with one set is rejected",
			constraints: together,
			callArgs:    `{ name: 'a' }`,
			wantErr: "resource.x: schema: constraints[0] (required-together " +
				"[name, size]): expected all set or all null, got 1 set (name)",
		},
		{
			name:        "required-together with both set passes",
			constraints: together,
			callArgs:    `{ name: 'a', size: 1 }`,
		},
		{
			name:        "required-together with neither set passes",
			constraints: together,
			callArgs:    `{}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := planCompositeConstraintErr(t, inputs, tt.constraints, tt.callArgs)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestPlanCompositeNestedConstraints(t *testing.T) {
	const inputs = `{
  code: { type: optional(object({ inline: optional(string), from-file: optional(string) })) }
  size: { type: optional(integer) }
}`
	const oneOf = `[ { kind: exactly-one-of, fields: [input.code.inline, input.code.from-file] } ]`
	const predicate = `[ { kind: predicate, when: input.code.inline != null,` +
		` require: input.size != null } ]`
	tests := []struct {
		name        string
		constraints string
		callArgs    string
		wantErr     string
	}{
		{
			name:        "exactly-one-of nested with one set passes",
			constraints: oneOf,
			callArgs:    `{ code: { inline: 'x' } }`,
		},
		{
			name:        "exactly-one-of nested with two set is rejected",
			constraints: oneOf,
			callArgs:    `{ code: { inline: 'x', from-file: 'y' } }`,
			wantErr: "resource.x: schema: constraints[0] (exactly-one-of " +
				"[code.inline, code.from-file]): expected exactly one to be set, " +
				"got 2 (code.inline, code.from-file)",
		},
		{
			name:        "exactly-one-of nested with parent unset is rejected",
			constraints: oneOf,
			callArgs:    `{}`,
			wantErr: "resource.x: schema: constraints[0] (exactly-one-of " +
				"[code.inline, code.from-file]): expected exactly one to be set, got 0 ()",
		},
		{
			name:        "predicate over nested with requirement met passes",
			constraints: predicate,
			callArgs:    `{ code: { inline: 'x' }, size: 1 }`,
		},
		{
			name:        "predicate over nested with unmet requirement is rejected",
			constraints: predicate,
			callArgs:    `{ code: { inline: 'x' } }`,
			wantErr: "resource.x: schema: constraints[0] (predicate): " +
				"predicate requirement not satisfied",
		},
		{
			name:        "predicate over unset nested parent passes",
			constraints: predicate,
			callArgs:    `{}`,
		},
		{
			name:        "predicate over present parent with unset leaf passes",
			constraints: predicate,
			callArgs:    `{ code: {} }`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := planCompositeConstraintErr(t, inputs, tt.constraints, tt.callArgs)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestPlanCompositeSplatConstraints(t *testing.T) {
	const inputs = `{
  replicas: {
    type: optional(list(object({ inline: optional(string), from-file: optional(string) })))
  }
}`
	const oneOf = `[ { kind: exactly-one-of,
  fields: [input.replicas[*].inline, input.replicas[*].from-file] } ]`
	tests := []struct {
		name     string
		callArgs string
		wantErr  string
	}{
		{
			name:     "every element conforming passes",
			callArgs: `{ replicas: [{ inline: 'x' }, { from-file: 'y' }] }`,
		},
		{
			name:     "a violating element is named by index",
			callArgs: `{ replicas: [{ inline: 'x' }, { inline: 'x', from-file: 'y' }] }`,
			wantErr: "resource.x: schema: constraints[0] (exactly-one-of " +
				"[replicas[1].inline, replicas[1].from-file]): expected exactly one " +
				"to be set, got 2 (replicas[1].inline, replicas[1].from-file)",
		},
		{
			name:     "an unset list checks nothing",
			callArgs: `{}`,
		},
		{
			name:     "an empty list checks nothing",
			callArgs: `{ replicas: [] }`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := planCompositeConstraintErr(t, inputs, oneOf, tt.callArgs)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestPlanCompositeConstraintCallsFunction(t *testing.T) {
	const inputs = `{
  replicas: { type: optional(list(object({ port: optional(integer) }))) }
}`
	const predicate = `[ {
  kind:    predicate
  when:    input.replicas != null
  require: core.all([for r in input.replicas: r.port > 0])
  message: 'every replica needs a positive port'
} ]`

	err := planCompositeConstraintErr(t, inputs, predicate,
		`{ replicas: [{ port: 443 }, { port: 0 }] }`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "every replica needs a positive port")

	err = planCompositeConstraintErr(t, inputs, predicate,
		`{ replicas: [{ port: 443 }, { port: 8080 }] }`)
	require.NoError(t, err)
}

func TestPlanForEachResourceEmitsOneStepPerInstance(t *testing.T) {
	src := planFixture(t, "plan-for-each-resource-emits-one-step-per-instance")
	var c resourceCounters
	libs := resourceModules(&c)
	exec := planTestExecutor(t, src, libs, newStateStore(t),
		state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"})
	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	alpha := stepFor(plan, "resource.many['alpha']")
	require.NotNil(t, alpha, "alpha instance step")
	require.Equal(t, DecisionCreate, alpha.Decision)
	require.Equal(t, "alpha", alpha.Inputs["name"])
	require.Equal(t, int64(1), alpha.Inputs["size"])

	beta := stepFor(plan, "resource.many['beta']")
	require.NotNil(t, beta, "beta instance step")
	require.Equal(t, DecisionCreate, beta.Decision)
	require.Equal(t, "beta", beta.Inputs["name"])
	require.Equal(t, int64(2), beta.Inputs["size"])

	require.Nil(t, stepFor(plan, "resource.many"),
		"no plan step for the template address itself")
}

func TestPlanForEachOrphanInstanceDestroyed(t *testing.T) {
	src := planFixture(t, "plan-for-each-orphan-instance-destroyed")
	var c resourceCounters
	libs := resourceModules(&c)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := planTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}}
	applyOnce(t, exec)

	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1)}}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	beta := stepFor(plan, "resource.many['beta']")
	require.NotNil(t, beta, "removed instance shows up as orphan")
	require.Equal(t, DecisionDestroy, beta.Decision)
}

func TestPlanComposite(t *testing.T) {
	composite := syntaxResourceComposite(t, "pair", planFixture(t, "plan-composite-1"))
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"pair": composite,
		},
	}
	stackSrc := planFixture(t, "plan-composite-2")
	plan := runPlan(t, stackSrc, libs, newStateStore(t))

	boundary := stepFor(plan, "resource.x")
	require.NotNil(t, boundary)
	require.True(t, boundary.Composite)
	require.Equal(t, NodeResource, boundary.Kind)
	require.Equal(t, DecisionEval, boundary.Decision)
	require.Equal(t, "alpha", boundary.Inputs["name"])

	one := stepFor(plan, "resource.x/resource.one")
	require.NotNil(t, one)
	require.Equal(t, NodeResource, one.Kind)
	require.Equal(t, DecisionCreate, one.Decision)
	require.Equal(t, "alpha", one.Inputs["name"])

	two := stepFor(plan, "resource.x/resource.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
}

func TestPlanCompositeInternalActionSkipsAfterRun(t *testing.T) {
	composite := syntaxResourceComposite(t, "box",
		planFixture(t, "plan-composite-internal-action-skips-after-run-1"))
	libs := testModules()
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": composite,
		},
	}
	stackSrc := planFixture(t, "plan-composite-internal-action-skips-after-run-2")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := planTestExecutor(t, stackSrc, libs, store, stack)
	applyOnce(t, exec)

	plan := runPlan(t, stackSrc, libs, store)
	step := stepFor(plan, "resource.x/action.say")
	require.NotNil(t, step,
		"internal action should appear as a plan step under its composite-prefixed address")
	require.Equal(t, NodeAction, step.Kind)
	require.Equal(t, DecisionSkip, step.Decision,
		"second plan should skip the internal action whose trigger hash matches state")
}

func TestPlanCreateForFreshResource(t *testing.T) {
	src := planFixture(t, "plan-create-for-fresh-resource")
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))
	require.Equal(t, DecisionCreate, decisionFor(plan, "resource.one"))
	require.Equal(t, int64(0), c.creates, "Plan should not invoke Create")
}

func TestPlanNoOpForUnchanged(t *testing.T) {
	src := planFixture(t, "plan-no-op-for-unchanged")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	plan := runPlan(t, src, libs, store)
	require.Equal(t, DecisionNoOp, decisionFor(plan, "resource.one"))
}

func TestPlanUpdateForNonReplaceFieldChange(t *testing.T) {
	first := planFixture(t, "plan-update-for-non-replace-field-change-1")
	second := planFixture(t, "plan-update-for-non-replace-field-change-2")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, first, libs, store, stack))

	plan := runPlan(t, second, libs, store)
	one := stepFor(plan, "resource.one")
	require.Equal(t, DecisionUpdate, one.Decision)
	require.Empty(t, one.ReplaceTriggers)
	require.Equal(t, float64(1), one.PriorInputs["size"],
		"the prior body is recorded (state round trip renders numbers as float)")
}

func TestPlanReplaceForReplaceFieldChange(t *testing.T) {
	first := planFixture(t, "plan-replace-for-replace-field-change-1")
	second := planFixture(t, "plan-replace-for-replace-field-change-2")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, first, libs, store, stack))

	plan := runPlan(t, second, libs, store)
	one := stepFor(plan, "resource.one")
	require.Equal(t, DecisionReplace, one.Decision)
	require.Equal(t, []string{"name"}, one.ReplaceTriggers, "the changed replace field is named")
	require.Equal(t, "alpha", one.PriorInputs["name"])
}

func TestPlanUpdateRevertsDrift(t *testing.T) {
	src := planFixture(t, "plan-update-reverts-drift")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	plan := runPlan(t, src, libs, store)
	step := stepFor(plan, "resource.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionUpdate, step.Decision,
		"drift with no input change should plan a revert via Update")
	require.True(t, step.Drift(), "step should report drift")
	require.NotEqual(t, step.PriorOutputs["size"], step.ObservedOutputs["size"])
}

func TestUpdateSeesObservedDriftAtApply(t *testing.T) {
	src := planFixture(t, "update-sees-observed-drift-at-apply")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	// First apply records outputs with size 1.
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	// Reality drifts to size 99; the re-apply plans a revert Update.
	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	require.NotNil(t, c.gotUpdatePrior)
	require.Equal(t, int64(1), c.gotUpdatePrior.Outputs.(map[string]any)["size"],
		"Outputs is the result recorded by the last apply")
	require.Equal(t, int64(99), c.gotUpdatePrior.Observed.(map[string]any)["size"],
		"Observed is what the plan-time Read saw, the drifted reality")
}

func TestPlanMigratesPriorOutputsOnSchemaBump(t *testing.T) {
	src := planFixture(t, "plan-migrates-prior-outputs-on-schema-bump")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[migratingCountingResource, any, any](
					func() *migratingCountingResource {
						return &migratingCountingResource{
							countingResource: countingResource{counters: &c},
						}
					},
				),
			},
		},
	}

	var seenByRead any
	c.readFn = func(prior any) (any, error) {
		seenByRead = prior
		return prior, nil
	}

	plan := runPlan(t, src, libs, store)
	step := stepFor(plan, "resource.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionNoOp, step.Decision)

	rcv, ok := seenByRead.(map[string]any)
	require.True(t, ok)
	require.NotContains(t, rcv, "id", "Read should see the migrated outputs")
	require.Equal(t, "fake-alpha", rcv["name-id"])
	require.NotContains(t, step.PriorOutputs, "id",
		"PriorOutputs on the plan step should be the migrated outputs")
	require.Equal(t, "fake-alpha", step.PriorOutputs["name-id"])
}

func TestPlanErrorsWhenSchemaBumpHasNoMigrate(t *testing.T) {
	src := planFixture(t, "plan-errors-when-schema-bump-has-no-migrate")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha"},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResourceV2, any, any](
					func() *countingResourceV2 {
						return &countingResourceV2{
							countingResource: countingResource{counters: &c},
						}
					},
				),
			},
		},
	}

	exec := planTestExecutor(t, src, libs, store, stack)
	_, err = exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no migration registered")
}

func TestPlanMigratesPriorInputsOnSchemaBump(t *testing.T) {
	// The prior entry was written at v1 with the old input field name
	// `label`. The current resource is v2 and its Migrate renames `label`
	// to `name`. After migration the prior inputs match the source, so the
	// plan is a no-op rather than a spurious update from diffing inputs
	// recorded under two different schema versions.
	src := planFixture(t, "plan-migrates-prior-inputs-on-schema-bump")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"label": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	plan := runPlan(t, src, inputMigratingLibs(&c), store)
	step := stepFor(plan, "resource.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionNoOp, step.Decision)
	require.NotContains(t, step.PriorInputs, "label",
		"PriorInputs on the plan step should be the migrated inputs")
	require.Equal(t, "alpha", step.PriorInputs["name"])
}

func TestApplyUpdateReceivesMigratedPriorInputs(t *testing.T) {
	// A schema bump renamed the input `label` to `name`. The prior entry
	// is v1; the source changes size, so the plan is an update. The Update
	// must see the migrated prior inputs (name set), not the raw v1 entry,
	// where the field was still `label` and would decode to the zero value.
	// The rewritten entry ends at the current version with current
	// inputs.
	src := planFixture(t, "apply-update-receives-migrated-prior-inputs")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"label": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	libs := inputMigratingLibs(&c)
	exec := planTestExecutor(t, src, libs, store, stack)
	_, err = planAndApply(exec)
	require.NoError(t, err)

	require.NotNil(t, c.gotInputMigratePrior)
	require.Equal(t, "alpha", c.gotInputMigratePrior.Inputs.Name,
		"Update should see the migrated prior input name, not the raw v1 entry")
	require.EqualValues(t, 1, c.gotInputMigratePrior.Inputs.Size)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.one")
	require.NotNil(t, ent)
	require.Equal(t, 2, ent.SchemaVersion)
	require.NotContains(t, ent.Inputs, "label")
	require.Equal(t, "alpha", ent.Inputs["name"])
	require.EqualValues(t, 2, ent.Inputs["size"])
}

// seedPrior writes entries as store's current snapshot, so a test can
// start a plan or apply from existing state.
func seedPrior(
	t *testing.T, store *local.Store, stack state.FactoryInfo, entries ...*state.Entry,
) {
	t.Helper()
	snap := state.NewSnapshot(stack, store.Stack())
	snap.Entries = entries
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
}

func TestPlanDefaultsOverlayPreventsSpuriousUpdate(t *testing.T) {
	// `size` gained a Value default after this resource was created, so the
	// prior entry has none. The default fills into the current body; the
	// overlay fills it into the prior too, so the diff sees them equal and
	// the plan stays a no-op instead of a vacuous update.
	src := planFixture(t, "plan-defaults-overlay-prevents-spurious-update")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	plan := runPlan(t, src, defaultingLibs(&c), store)
	step := stepFor(plan, "resource.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionNoOp, step.Decision)
	require.EqualValues(t, 7, step.PriorInputs["size"],
		"the declared default should be overlaid onto the prior inputs")
}

func TestPlanDefaultsOverlayKeepsExplicitPriorValue(t *testing.T) {
	// The prior set size explicitly to 3. The body now omits size, so the
	// default 7 is the desired value -- a real update. The overlay fills
	// only missing fields, so the prior is still seen as 3 and the change
	// is genuine, not invented.
	src := planFixture(t, "plan-defaults-overlay-keeps-explicit-prior-value")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": float64(3)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(3)},
	})

	var c resourceCounters
	plan := runPlan(t, src, defaultingLibs(&c), store)
	step := stepFor(plan, "resource.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionUpdate, step.Decision)
	require.EqualValues(t, 3, step.PriorInputs["size"],
		"a value the prior actually set must survive the overlay")
	require.EqualValues(t, 7, step.Inputs["size"])
}

func TestApplyDefaultsOverlayAdditiveFieldMakesNoCloudUpdate(t *testing.T) {
	// End to end: a defaulted field added after creation should not provoke
	// a cloud update. The plan is a no-op, Update is never called, and the
	// apply records the resolved default so the next plan agrees.
	src := planFixture(t, "apply-defaults-overlay-additive-field-makes-no-cloud-update")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := planTestExecutor(t, src, libs, store, stack)
	_, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates), "no cloud update")
	require.Equal(t, int64(0), atomic.LoadInt64(&c.creates))

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.one")
	require.NotNil(t, ent)
	require.EqualValues(t, 7, ent.Inputs["size"], "apply records the resolved default")

	plan2, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, stepFor(plan2, "resource.one").Decision)
}

func TestApplyDefaultsOverlayUpdateSeesFilledPriorDefault(t *testing.T) {
	// The prior predates `size`, and the body now sets it to 9. The plan is
	// an update, and the overlay means Update sees a prior of 7 (the
	// declared default), not a zero value from an absent field.
	src := planFixture(t, "apply-defaults-overlay-update-sees-filled-prior-default")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.one",
		Type:          state.EntryLeaf,
		Kind:          "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := planTestExecutor(t, src, libs, store, stack)
	_, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, int64(1), atomic.LoadInt64(&c.updates))
	require.NotNil(t, c.gotUpdatePrior)
	require.EqualValues(t, 7, c.gotUpdatePrior.Inputs.Size,
		"Update should see the overlaid default as the prior, not zero")

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.one")
	require.NotNil(t, ent)
	require.EqualValues(t, 9, ent.Inputs["size"])
}

func TestApplyDefaultsOverlayForEachIsNoOp(t *testing.T) {
	// The overlay also covers @for-each instances: each prior instance
	// predates `size`, so each is a no-op once the default is overlaid.
	src := planFixture(t, "apply-defaults-overlay-for-each-is-no-op")
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		&state.Entry{
			Address:       "resource.many['alpha']",
			Type:          state.EntryLeaf,
			Kind:          "resource",
			Binding:       &state.Binding{Alias: "core", Export: "thing"},
			SchemaVersion: 1,
			Inputs:        map[string]any{"name": "alpha"},
			Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
		},
		&state.Entry{
			Address:       "resource.many['beta']",
			Type:          state.EntryLeaf,
			Kind:          "resource",
			Binding:       &state.Binding{Alias: "core", Export: "thing"},
			SchemaVersion: 1,
			Inputs:        map[string]any{"name": "beta"},
			Outputs:       map[string]any{"id": "fake-beta", "name": "beta"},
		},
	)

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := planTestExecutor(t, src, libs, store, stack)
	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, stepFor(plan, "resource.many['alpha']").Decision)
	require.Equal(t, DecisionNoOp, stepFor(plan, "resource.many['beta']").Decision)

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates), "for-each instances are no-ops")
}

func TestPlanRecordsUnresolvedFieldRefs(t *testing.T) {
	src := planFixture(t, "plan-records-unresolved-field-refs")
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	two := stepFor(plan, "resource.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.Equal(t, []string{"resource.one.id"}, two.UnresolvedInputs["name"])
	require.NotContains(t, two.UnresolvedInputs, "size",
		"resolved fields should not appear in UnresolvedInputs")
	require.Equal(t, PendingValue{Refs: []string{"resource.one.id"}}, two.Inputs["name"],
		"an unresolved field keeps a placeholder the renderer shows in place")
	require.Equal(t, int64(2), two.Inputs["size"])
}

func TestPartialValueKeepsListStructure(t *testing.T) {
	expr := parseValue(t, "['lit', resource.one.id]")
	got := partialValue(expr, &EvalContext{}, nil)
	require.Equal(t, []any{
		"lit",
		PendingValue{Refs: []string{"resource.one.id"}},
	}, got)
}

func TestPartialValueKeepsObjectStructure(t *testing.T) {
	expr := parseValue(t, "{ ready: true, id: resource.one.id }")
	got := partialValue(expr, &EvalContext{}, nil)
	require.Equal(t, map[string]any{
		"ready": true,
		"id":    PendingValue{Refs: []string{"resource.one.id"}},
	}, got)
}

func TestPlanResolvesInputRefAtPlanTime(t *testing.T) {
	src := planFixture(t, "plan-resolves-input-ref-at-plan-time")
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	two := stepFor(plan, "resource.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.NotContains(t, two.UnresolvedInputs, "name",
		"a reference to an upstream input is known at plan, not deferred to apply")
	require.Equal(t, "alpha", two.Inputs["name"])
}

func TestPlanDoesNotResolveAPendingInput(t *testing.T) {
	src := planFixture(t, "plan-does-not-resolve-a-pending-input")
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	three := stepFor(plan, "resource.three")
	require.NotNil(t, three)
	require.Equal(t, []string{"resource.two.name"}, three.UnresolvedInputs["name"],
		"two.name is itself waiting on one.id, so reading it stays unknown at plan")
	require.Equal(t,
		PendingValue{Refs: []string{"resource.two.name"}}, three.Inputs["name"])
}

func TestPlanExpandsLocalInUnresolvedRefs(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, planFixture(t, "plan-expands-local-in-unresolved-refs"))
	var c resourceCounters
	libs := resourceModules(&c)
	exec := &Executor{
		DAG:          BuildSyntaxDAG(fixture.body, libs),
		SyntaxSource: &fixture.body,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	two := stepFor(plan, "resource.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.Equal(t, []string{"resource.one.id"}, two.UnresolvedInputs["name"],
		"a field reading a local should show the resource the local waits on")
	require.Equal(t, PendingValue{Refs: []string{"resource.one.id"}}, two.Inputs["name"])
}

func TestUpgradeActionRerunFollowsLocal(t *testing.T) {
	src := planFixture(t, "upgrade-action-rerun-follows-local")
	body := syntaxFactoryBody(t, src)
	dag := BuildSyntaxDAG(body, nil)
	sl := newScopeLocals(syntaxLocalMap(body.Locals), dag.Nodes)

	cases := []struct {
		name     string
		upstream Decision
		want     Decision
	}{
		{"upstream updated", DecisionUpdate, DecisionRerun},
		{"upstream unchanged", DecisionNoOp, DecisionSkip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := []*PlanStep{
				{Address: "resource.one", Kind: NodeResource, Decision: tc.upstream},
				{Address: "action.notify", Kind: NodeAction, Decision: DecisionSkip},
			}
			upgradeActionRerun(steps, dag, sl)
			require.Equal(t, tc.want, steps[1].Decision)
		})
	}
}

func TestPlanCreateWhenResourceIsGone(t *testing.T) {
	src := planFixture(t, "plan-create-when-resource-is-gone")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	plan := runPlan(t, src, libs, store)
	step := stepFor(plan, "resource.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionCreate, step.Decision,
		"a missing resource with prior state should plan a recreate")
	require.True(t, step.Gone(), "step should report Gone")
	require.Empty(t, step.ObservedOutputs)
}

func TestPlanDestroyForOrphan(t *testing.T) {
	first := planFixture(t, "plan-destroy-for-orphan-1")
	second := planFixture(t, "plan-destroy-for-orphan-2")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, first, libs, store, stack))

	plan := runPlan(t, second, libs, store)
	require.Equal(t, DecisionNoOp, decisionFor(plan, "resource.keep"))
	require.Equal(t, DecisionDestroy, decisionFor(plan, "resource.orph"))
}

func TestPlanRerunForChangedAction(t *testing.T) {
	first := planFixture(t, "plan-rerun-for-changed-action-1")
	second := planFixture(t, "plan-rerun-for-changed-action-2")
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeAction[echoAction, any, any](),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, first, libs, store, stack))

	plan := runPlan(t, second, libs, store)
	require.Equal(t, DecisionRerun, decisionFor(plan, "action.hi"))
}

func TestPlanSkipForUnchangedAction(t *testing.T) {
	src := planFixture(t, "plan-skip-for-unchanged-action")
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeAction[echoAction, any, any](),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	plan := runPlan(t, src, libs, store)
	require.Equal(t, DecisionSkip, decisionFor(plan, "action.hi"))
}

func TestPlanRecordsStateRev(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, map[string]*Library{}, store, stack))

	plan := runPlan(t, src, map[string]*Library{}, store)
	require.NotEmpty(t, plan.StateRev)
}

// planResourcesSrc builds a stack with n core.thing resources named
// r0..r(n-1) so the parallel-read tests can dial the fan-out.
func planResourcesSrc(n int) string {
	var src strings.Builder
	fmt.Fprintf(&src, "%s: {\n", "resources")
	for i := range n {
		fmt.Fprintf(&src, "  r%d: core.thing { name: 'r%d', size: %d }\n", i, i, i)
	}
	src.WriteString("}\n")
	return src.String()
}

func TestPlanReadsResourcesInParallel(t *testing.T) {
	const n = 6
	src := planResourcesSrc(n)

	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	const delay = 150 * time.Millisecond
	c.readFn = func(prior any) (any, error) {
		time.Sleep(delay)
		return prior, nil
	}

	exec := planTestExecutor(t, src, libs, store, stack)
	exec.Parallelism = n
	start := time.Now()
	plan, err := exec.Plan(context.Background())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Len(t, plan.Steps, n)
	require.Less(t, elapsed, time.Duration(n-1)*delay,
		"parallel plan took %s; expected well under %s for serial",
		elapsed, time.Duration(n-1)*delay)
}

func TestPlanReadsAreSerialAtP1(t *testing.T) {
	const n = 4
	src := planResourcesSrc(n)

	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	const delay = 80 * time.Millisecond
	c.readFn = func(prior any) (any, error) {
		time.Sleep(delay)
		return prior, nil
	}

	exec := planTestExecutor(t, src, libs, store, stack)
	exec.Parallelism = 1
	start := time.Now()
	_, err := exec.Plan(context.Background())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.GreaterOrEqual(t, elapsed, time.Duration(n)*delay,
		"serial plan took %s; expected at least %s", elapsed, time.Duration(n)*delay)
}

func TestPlanPropagatesReadError(t *testing.T) {
	src := planFixture(t, "plan-propagates-read-error")
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, planTestExecutor(t, src, libs, store, stack))

	wantErr := errors.New("cloud is unwell")
	c.readFn = func(any) (any, error) { return nil, wantErr }

	exec := planTestExecutor(t, src, libs, store, stack)
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, wantErr)
	require.Contains(t, err.Error(), "resource.one")
}

func TestPartialValueKeepsStringKeyedFields(t *testing.T) {
	expr := parseValue(t, "{ 'app/role': 'web', id: resource.one.id }")
	got := partialValue(expr, &EvalContext{}, nil)
	require.Equal(t, map[string]any{
		"app/role": "web",
		"id":       PendingValue{Refs: []string{"resource.one.id"}},
	}, got)
}
