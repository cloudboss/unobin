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

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

// planThingConstraintErr plans a stack with one core.thing resource whose
// body is the given literal object, after attaching specs to the thing
// type's constraints, and returns the plan error (nil when it succeeds).
func planThingConstraintErr(t *testing.T, specs []lang.ConstraintSpec, body string) error {
	t.Helper()
	libs := resourceModules(&resourceCounters{})
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{"resource.thing": specs}
	src := "resources: {\n  core.thing.x: " + body + "\n}\n"
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
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
const goConstraintPrefix = "resource.core.thing.x: schema: "

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
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + "constraints[0] (exactly-one-of [name, size]): " +
				"expected exactly one to be set, got 2 (name, size)",
		},
		{
			name:  "exactly-one-of with one set passes",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.name", "var.size")},
			body:  `{ name: 'a' }`,
		},
		{
			name:  "exactly-one-of with none set is rejected",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.name", "var.size")},
			body:  `{ region: 'us' }`,
			wantErr: goConstraintPrefix + "constraints[0] (exactly-one-of [name, size]): " +
				"expected exactly one to be set, got 0 ()",
		},
		{
			name:  "at-least-one-of with none set is rejected",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "var.name", "var.size")},
			body:  `{ region: 'us' }`,
			wantErr: goConstraintPrefix + "constraints[0] (at-least-one-of [name, size]): " +
				"expected at least one to be set, got none",
		},
		{
			name:  "at-least-one-of with both set passes",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "at-most-one-of with two set is rejected",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + "constraints[0] (at-most-one-of [name, size]): " +
				"expected at most one to be set, got 2 (name, size)",
		},
		{
			name:  "at-most-one-of with none set passes",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "var.name", "var.size")},
			body:  `{ region: 'us' }`,
		},
		{
			name:  "at-most-one-of with two set is rejected",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + "constraints[0] (at-most-one-of [name, size]): " +
				"expected at most one to be set, got 2 (name, size)",
		},
		{
			name:  "required-together with one set is rejected",
			specs: []lang.ConstraintSpec{setSpec("required-together", "var.name", "var.size")},
			body:  `{ name: 'a' }`,
			wantErr: goConstraintPrefix + "constraints[0] (required-together [name, size]): " +
				"expected all set or all null, got 1 set (name)",
		},
		{
			name:  "required-together with both set passes",
			specs: []lang.ConstraintSpec{setSpec("required-together", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "required-together with neither set passes",
			specs: []lang.ConstraintSpec{setSpec("required-together", "var.name", "var.size")},
			body:  `{ region: 'us' }`,
		},
		{
			name:  "required-with trigger lacking dependent is rejected",
			specs: []lang.ConstraintSpec{setSpec("required-with", "var.name", "var.size")},
			body:  `{ name: 'a' }`,
			wantErr: goConstraintPrefix + `constraints[0] (required-with): ` +
				`"name" is set, so [size] must also be set; missing size`,
		},
		{
			name:  "required-with trigger with dependent passes",
			specs: []lang.ConstraintSpec{setSpec("required-with", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "required-with without trigger passes",
			specs: []lang.ConstraintSpec{setSpec("required-with", "var.name", "var.size")},
			body:  `{ size: 1 }`,
		},
		{
			name:  "forbidden-with trigger and forbidden field is rejected",
			specs: []lang.ConstraintSpec{setSpec("forbidden-with", "var.name", "var.size")},
			body:  `{ name: 'a', size: 1 }`,
			wantErr: goConstraintPrefix + `constraints[0] (forbidden-with): ` +
				`"name" is set, so [size] must be null; got size`,
		},
		{
			name:  "forbidden-with trigger alone passes",
			specs: []lang.ConstraintSpec{setSpec("forbidden-with", "var.name", "var.size")},
			body:  `{ name: 'a' }`,
		},
		{
			name:    "predicate with unmet requirement is rejected",
			specs:   []lang.ConstraintSpec{predSpec("var.name != null", "var.size != null")},
			body:    `{ name: 'a' }`,
			wantErr: goConstraintPrefix + "constraints[0] (predicate): predicate requirement not satisfied",
		},
		{
			name:  "predicate with met requirement passes",
			specs: []lang.ConstraintSpec{predSpec("var.name != null", "var.size != null")},
			body:  `{ name: 'a', size: 1 }`,
		},
		{
			name:  "predicate whose condition is false passes",
			specs: []lang.ConstraintSpec{predSpec("var.name != null", "var.size != null")},
			body:  `{ size: 1 }`,
		},
		{
			name: "predicate uses its custom message",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", When: "var.name != null",
				Require: "var.size != null", Message: "size is required with name",
			}},
			body:    `{ name: 'a' }`,
			wantErr: goConstraintPrefix + "constraints[0] (predicate): size is required with name",
		},
		{
			name: "two violated constraints report both",
			specs: []lang.ConstraintSpec{
				setSpec("exactly-one-of", "var.name", "var.size"),
				setSpec("forbidden-with", "var.name", "var.size"),
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
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.items[*].a", "var.items[*].b")},
			body:  `{ items: [{ a: 1 }, { a: 1, b: 2 }] }`,
			wantErr: goConstraintPrefix +
				"constraints[0] (exactly-one-of [items[1].a, items[1].b]): " +
				"expected exactly one to be set, got 2 (items[1].a, items[1].b)",
		},
		{
			name:  "splat constraint passes when every element conforms",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.items[*].a", "var.items[*].b")},
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
	libs["core"].Resources["plain"] = MakeResourceWith[countingResource, any](
		func() *countingResource { return &countingResource{counters: c} },
	)
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{"resource.thing": specs}
	src := "resources: {\n  core.plain.a: { name: 'a' }\n" +
		"  core.thing.b: " + body + "\n}\n"
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	_, err := exec.Plan(context.Background())
	return err
}

const forwardConstraintPrefix = "resource.core.thing.b: schema: "

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
	ref := "resource.core.plain.a.id"
	return []struct {
		name    string
		specs   []lang.ConstraintSpec
		body    string
		wantErr string
	}{
		{
			name:  "exactly-one-of defers on a pending field",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.name", "var.size")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name:  "at-least-one-of defers on a pending field",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "var.name", "var.zone")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name: "at-most-one-of defers although known fields violate",
			specs: []lang.ConstraintSpec{
				setSpec("at-most-one-of", "var.name", "var.region", "var.zone"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
		},
		{
			name: "at-most-one-of defers although known fields violate",
			specs: []lang.ConstraintSpec{
				setSpec("at-most-one-of", "var.name", "var.region", "var.zone"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
		},
		{
			name:  "required-together defers on a pending field",
			specs: []lang.ConstraintSpec{setSpec("required-together", "var.name", "var.region")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
		},
		{
			name:  "required-with defers on a pending dependent",
			specs: []lang.ConstraintSpec{setSpec("required-with", "var.region", "var.name")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
		},
		{
			name: "forbidden-with defers although a known field violates",
			specs: []lang.ConstraintSpec{
				setSpec("forbidden-with", "var.region", "var.name", "var.zone"),
			},
			body: `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
		},
		{
			name:  "predicate defers when require reads the pending field",
			specs: []lang.ConstraintSpec{predSpec("true", "var.name != null")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name:  "predicate defers when when reads the pending field",
			specs: []lang.ConstraintSpec{predSpec("var.name == null", "var.region != null")},
			body:  `{ name: ` + ref + ` }`,
		},
		{
			name: "iterating predicate defers when require reads the pending field",
			specs: []lang.ConstraintSpec{{
				Kind: "predicate", ForEach: "var.regions",
				When: "true", Require: "var.name != null",
			}},
			body: `{ name: ` + ref + `, regions: ['us', 'eu'] }`,
		},
		{
			name:  "exactly-one-of reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("exactly-one-of", "var.region", "var.zone")},
			body:  `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (exactly-one-of [region, zone]): " +
				"expected exactly one to be set, got 2 (region, zone)",
		},
		{
			name:  "at-least-one-of reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("at-least-one-of", "var.region", "var.zone")},
			body:  `{ name: ` + ref + ` }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (at-least-one-of [region, zone]): " +
				"expected at least one to be set, got none",
		},
		{
			name:  "at-most-one-of reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("at-most-one-of", "var.region", "var.zone")},
			body:  `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (at-most-one-of [region, zone]): " +
				"expected at most one to be set, got 2 (region, zone)",
		},
		{
			name:  "required-together reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("required-together", "var.region", "var.zone")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (required-together [region, zone]): " +
				"expected all set or all null, got 1 set (region)",
		},
		{
			name:  "required-with reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("required-with", "var.region", "var.zone")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
			wantErr: forwardConstraintPrefix +
				`constraints[0] (required-with): ` +
				`"region" is set, so [zone] must also be set; missing zone`,
		},
		{
			name:  "forbidden-with reports beside a pending field",
			specs: []lang.ConstraintSpec{setSpec("forbidden-with", "var.region", "var.zone")},
			body:  `{ name: ` + ref + `, region: 'us', zone: 'z' }`,
			wantErr: forwardConstraintPrefix +
				`constraints[0] (forbidden-with): ` +
				`"region" is set, so [zone] must be null; got zone`,
		},
		{
			name:  "predicate reports beside a pending field",
			specs: []lang.ConstraintSpec{predSpec("var.region != null", "var.zone != null")},
			body:  `{ name: ` + ref + `, region: 'us' }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (predicate): predicate requirement not satisfied",
		},
		{
			name: "splat rule reports when its list resolved",
			specs: []lang.ConstraintSpec{
				setSpec("exactly-one-of", "var.items[*].a", "var.items[*].b"),
			},
			body: `{ name: ` + ref + `, items: [{ a: 1, b: 2 }] }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (exactly-one-of [items[0].a, items[0].b]): " +
				"expected exactly one to be set, got 2 (items[0].a, items[0].b)",
		},
		{
			name: "indexed rule reports when its list resolved",
			specs: []lang.ConstraintSpec{
				setSpec("required-together", "var.items[0].a", "var.items[0].b"),
			},
			body: `{ name: ` + ref + `, items: [{ a: 1 }] }`,
			wantErr: forwardConstraintPrefix +
				"constraints[0] (required-together [items[0].a, items[0].b]): " +
				"expected all set or all null, got 1 set (items[0].a)",
		},
		{
			name: "one rule reports while the others defer",
			specs: []lang.ConstraintSpec{
				setSpec("exactly-one-of", "var.region", "var.zone"),
				setSpec("exactly-one-of", "var.name", "var.size"),
				predSpec("true", "var.name != null"),
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
			{Name: "@rule", In: "var.rules"},
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
		"resource.core.thing.x: schema: constraints[0] (predicate): "+
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
	libs["core"].Actions = map[string]ActionRegistration{"echo": MakeAction[echoAction, any]()}
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{
		"action.echo": {{Kind: "exactly-one-of", Fields: []string{"var.name", "var.size"}}},
	}
	src := `
actions: { core.echo.x: { name: 'a', size: 1 } }
`
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	_, err := exec.Plan(context.Background())
	require.EqualError(t, err,
		"action.core.echo.x: schema: constraints[0] (exactly-one-of [name, size]): "+
			"expected exactly one to be set, got 2 (name, size)")
}

func runPlan(
	t *testing.T, src string, libraries map[string]*Library, store *localstate.LocalStore,
) *Plan {
	t.Helper()
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libraries),
		Libraries: libraries,
		Store:     store,
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
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
	composite := parseStack(t, `
inputs: `+inputs+`
constraints: `+constraints+`
resources: { core.thing.one: { name: 'fixed', size: 1 } }
`)
	libs := resourceModules(&resourceCounters{})
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"pair": {Name: "pair", Body: composite},
		},
	}
	src := `
resources: {
  w.pair.x: ` + callArgs + `
}
`
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	_, err := exec.Plan(context.Background())
	return err
}

func TestPlanCompositeConstraints(t *testing.T) {
	const inputs = `{
  name: { type: optional(string) }
  size: { type: optional(integer) }
}`
	const oneOf = `[ { kind: exactly-one-of, fields: [var.name, var.size] } ]`
	const together = `[ { kind: required-together, fields: [var.name, var.size] } ]`
	const predicate = `[ { kind: predicate, when: var.name != null, require: var.size != null } ]`
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
			wantErr: "resource.w.pair.x: schema: constraints[0] (exactly-one-of " +
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
			wantErr: "resource.w.pair.x: schema: constraints[0] (predicate): " +
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
			wantErr: "resource.w.pair.x: schema: constraints[0] (required-together " +
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
	const oneOf = `[ { kind: exactly-one-of, fields: [var.code.inline, var.code.from-file] } ]`
	const predicate = `[ { kind: predicate, when: var.code.inline != null,` +
		` require: var.size != null } ]`
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
			wantErr: "resource.w.pair.x: schema: constraints[0] (exactly-one-of " +
				"[code.inline, code.from-file]): expected exactly one to be set, " +
				"got 2 (code.inline, code.from-file)",
		},
		{
			name:        "exactly-one-of nested with parent unset is rejected",
			constraints: oneOf,
			callArgs:    `{}`,
			wantErr: "resource.w.pair.x: schema: constraints[0] (exactly-one-of " +
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
			wantErr: "resource.w.pair.x: schema: constraints[0] (predicate): " +
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
  fields: [var.replicas[*].inline, var.replicas[*].from-file] } ]`
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
			wantErr: "resource.w.pair.x: schema: constraints[0] (exactly-one-of " +
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
  when:    var.replicas != null
  require: core.all([for r in var.replicas: r.port > 0])
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
	src := `
resources: { core.thing.many: { @for-each: var.configs, name: @each.key, size: @each.value } }
`
	var c resourceCounters
	libs := resourceModules(&c)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs:    map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	alpha := stepFor(plan, "resource.core.thing.many['alpha']")
	require.NotNil(t, alpha, "alpha instance step")
	require.Equal(t, DecisionCreate, alpha.Decision)
	require.Equal(t, "alpha", alpha.Inputs["name"])
	require.Equal(t, int64(1), alpha.Inputs["size"])

	beta := stepFor(plan, "resource.core.thing.many['beta']")
	require.NotNil(t, beta, "beta instance step")
	require.Equal(t, DecisionCreate, beta.Decision)
	require.Equal(t, "beta", beta.Inputs["name"])
	require.Equal(t, int64(2), beta.Inputs["size"])

	require.Nil(t, stepFor(plan, "resource.core.thing.many"),
		"no plan step for the template address itself")
}

func TestPlanForEachOrphanInstanceDestroyed(t *testing.T) {
	src := `
resources: { core.thing.many: { @for-each: var.configs, name: @each.key, size: @each.value } }
`
	var c resourceCounters
	libs := resourceModules(&c)
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs:    map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:     store,
		Factory:   stack,
	}
	applyOnce(t, exec)

	exec.Inputs = map[string]any{"configs": map[string]any{"alpha": int64(1)}}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	beta := stepFor(plan, "resource.core.thing.many['beta']")
	require.NotNil(t, beta, "removed instance shows up as orphan")
	require.Equal(t, DecisionDestroy, beta.Decision)
}

func TestPlanComposite(t *testing.T) {
	composite := parseStack(t, `
resources: {
  core.thing.one: { name: var.name, size: 1 }
  core.thing.two: { name: var.name, size: 2 }
}
`)
	var c resourceCounters
	libs := resourceModules(&c)
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"pair": {Name: "pair", Body: composite},
		},
	}
	stackSrc := `
resources: { w.pair.x: { name: 'alpha' } }
`
	plan := runPlan(t, stackSrc, libs, newStateStore(t))

	boundary := stepFor(plan, "resource.w.pair.x")
	require.NotNil(t, boundary)
	require.True(t, boundary.Composite)
	require.Equal(t, NodeResource, boundary.Kind)
	require.Equal(t, DecisionEval, boundary.Decision)
	require.Equal(t, "alpha", boundary.Inputs["name"])

	one := stepFor(plan, "resource.w.pair.x/resource.core.thing.one")
	require.NotNil(t, one)
	require.Equal(t, NodeResource, one.Kind)
	require.Equal(t, DecisionCreate, one.Decision)
	require.Equal(t, "alpha", one.Inputs["name"])

	two := stepFor(plan, "resource.w.pair.x/resource.core.thing.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
}

func TestPlanCompositeInternalActionSkipsAfterRun(t *testing.T) {
	composite := parseStack(t, `
inputs:  { phrase: { type: string } }
actions: { core.echo.say: { echo: var.phrase } }
outputs: { said: { value: action.core.echo.say.echo } }
`)
	libs := testModules()
	libs["w"] = &Library{
		Name: "w",
		ResourceComposites: map[string]*CompositeType{
			"box": {Name: "box", Body: composite},
		},
	}
	stackSrc := `
resources: { w.box.x: { phrase: 'hello' } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, stackSrc), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	applyOnce(t, exec)

	plan := runPlan(t, stackSrc, libs, store)
	step := stepFor(plan, "resource.w.box.x/action.core.echo.say")
	require.NotNil(t, step,
		"internal action should appear as a plan step under its composite-prefixed address")
	require.Equal(t, NodeAction, step.Kind)
	require.Equal(t, DecisionSkip, step.Decision,
		"second plan should skip the internal action whose trigger hash matches state")
}

func TestPlanCreateForFreshResource(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))
	require.Equal(t, DecisionCreate, decisionFor(plan, "resource.core.thing.one"))
	require.Equal(t, int64(0), c.creates, "Plan should not invoke Create")
}

func TestPlanNoOpForUnchanged(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	plan := runPlan(t, src, libs, store)
	require.Equal(t, DecisionNoOp, decisionFor(plan, "resource.core.thing.one"))
}

func TestPlanUpdateForNonReplaceFieldChange(t *testing.T) {
	first := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	second := `
resources: { core.thing.one: { name: 'alpha', size: 99 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})

	plan := runPlan(t, second, libs, store)
	one := stepFor(plan, "resource.core.thing.one")
	require.Equal(t, DecisionUpdate, one.Decision)
	require.Empty(t, one.ReplaceTriggers)
	require.Equal(t, float64(1), one.PriorInputs["size"],
		"the prior body is recorded (state round trip renders numbers as float)")
}

func TestPlanReplaceForReplaceFieldChange(t *testing.T) {
	first := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	second := `
resources: { core.thing.one: { name: 'beta', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})

	plan := runPlan(t, second, libs, store)
	one := stepFor(plan, "resource.core.thing.one")
	require.Equal(t, DecisionReplace, one.Decision)
	require.Equal(t, []string{"name"}, one.ReplaceTriggers, "the changed replace field is named")
	require.Equal(t, "alpha", one.PriorInputs["name"])
}

func TestPlanUpdateRevertsDrift(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}

	plan := runPlan(t, src, libs, store)
	step := stepFor(plan, "resource.core.thing.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionUpdate, step.Decision,
		"drift with no input change should plan a revert via Update")
	require.True(t, step.Drift(), "step should report drift")
	require.NotEqual(t, step.PriorOutputs["size"], step.ObservedOutputs["size"])
}

func TestUpdateSeesObservedDriftAtApply(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	// First apply records outputs with size 1.
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	// Reality drifts to size 99; the re-apply plans a revert Update.
	c.readFn = func(prior any) (any, error) {
		m, _ := prior.(map[string]any)
		out := map[string]any{}
		maps.Copy(out, m)
		out["size"] = int64(99)
		return out, nil
	}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	require.NotNil(t, c.gotUpdatePrior)
	require.Equal(t, int64(1), c.gotUpdatePrior.Outputs.(map[string]any)["size"],
		"Outputs is the result recorded by the last apply")
	require.Equal(t, int64(99), c.gotUpdatePrior.Observed.(map[string]any)["size"],
		"Observed is what the plan-time Read saw, the drifted reality")
}

func TestPlanMigratesPriorOutputsOnSchemaBump(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
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
				"thing": MakeResourceWith[migratingCountingResource, any](
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
	step := stepFor(plan, "resource.core.thing.one")
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
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
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
				"thing": MakeResourceWith[countingResourceV2, any](
					func() *countingResourceV2 {
						return &countingResourceV2{
							countingResource: countingResource{counters: &c},
						}
					},
				),
			},
		},
	}

	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
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
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"label": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	plan := runPlan(t, src, inputMigratingLibs(&c), store)
	step := stepFor(plan, "resource.core.thing.one")
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
	src := `
resources: { core.thing.one: { name: 'alpha', size: 2 } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}

	prior := state.NewSnapshot(stack, store.Stack())
	prior.Entries = []*state.Entry{{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"label": "alpha", "size": float64(1)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(1)},
	}}
	rev, err := store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	var c resourceCounters
	libs := inputMigratingLibs(&c)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Store:     store,
		Factory:   stack,
	}
	_, err = planAndApply(exec)
	require.NoError(t, err)

	require.NotNil(t, c.gotInputMigratePrior)
	require.Equal(t, "alpha", c.gotInputMigratePrior.Inputs.Name,
		"Update should see the migrated prior input name, not the raw v1 entry")
	require.EqualValues(t, 1, c.gotInputMigratePrior.Inputs.Size)

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.core.thing.one")
	require.NotNil(t, ent)
	require.Equal(t, 2, ent.SchemaVersion)
	require.NotContains(t, ent.Inputs, "label")
	require.Equal(t, "alpha", ent.Inputs["name"])
	require.EqualValues(t, 2, ent.Inputs["size"])
}

// seedPrior writes entries as store's current snapshot, so a test can
// start a plan or apply from existing state.
func seedPrior(
	t *testing.T, store *localstate.LocalStore, stack state.FactoryInfo, entries ...*state.Entry,
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
	src := `
resources: { core.thing.one: { name: 'alpha' } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	plan := runPlan(t, src, defaultingLibs(&c), store)
	step := stepFor(plan, "resource.core.thing.one")
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
	src := `
resources: { core.thing.one: { name: 'alpha' } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha", "size": float64(3)},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha", "size": float64(3)},
	})

	var c resourceCounters
	plan := runPlan(t, src, defaultingLibs(&c), store)
	step := stepFor(plan, "resource.core.thing.one")
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
	src := `
resources: { core.thing.one: { name: 'alpha' } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates), "no cloud update")
	require.Equal(t, int64(0), atomic.LoadInt64(&c.creates))

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.core.thing.one")
	require.NotNil(t, ent)
	require.EqualValues(t, 7, ent.Inputs["size"], "apply records the resolved default")

	plan2, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, stepFor(plan2, "resource.core.thing.one").Decision)
}

func TestApplyDefaultsOverlayUpdateSeesFilledPriorDefault(t *testing.T) {
	// The prior predates `size`, and the body now sets it to 9. The plan is
	// an update, and the overlay means Update sees a prior of 7 (the
	// declared default), not a zero value from an absent field.
	src := `
resources: { core.thing.one: { name: 'alpha', size: 9 } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack, &state.Entry{
		Address:       "resource.core.thing.one",
		Type:          state.EntryLeaf,
		Kind:          "thing",
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": "alpha"},
		Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
	})

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	_, err := planAndApply(exec)
	require.NoError(t, err)
	require.Equal(t, int64(1), atomic.LoadInt64(&c.updates))
	require.NotNil(t, c.gotUpdatePrior)
	require.EqualValues(t, 7, c.gotUpdatePrior.Inputs.Size,
		"Update should see the overlaid default as the prior, not zero")

	snap, err := store.Current()
	require.NoError(t, err)
	ent := snap.Find("resource.core.thing.one")
	require.NotNil(t, ent)
	require.EqualValues(t, 9, ent.Inputs["size"])
}

func TestApplyDefaultsOverlayForEachIsNoOp(t *testing.T) {
	// The overlay also covers @for-each instances: each prior instance
	// predates `size`, so each is a no-op once the default is overlaid.
	src := `
resources: { core.thing.many: { @for-each: var.configs, name: @each.key } }
`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	seedPrior(t, store, stack,
		&state.Entry{
			Address:       "resource.core.thing.many['alpha']",
			Type:          state.EntryLeaf,
			Kind:          "thing",
			SchemaVersion: 1,
			Inputs:        map[string]any{"name": "alpha"},
			Outputs:       map[string]any{"id": "fake-alpha", "name": "alpha"},
		},
		&state.Entry{
			Address:       "resource.core.thing.many['beta']",
			Type:          state.EntryLeaf,
			Kind:          "thing",
			SchemaVersion: 1,
			Inputs:        map[string]any{"name": "beta"},
			Outputs:       map[string]any{"id": "fake-beta", "name": "beta"},
		},
	)

	var c resourceCounters
	libs := defaultingLibs(&c)
	exec := &Executor{
		DAG:       BuildDAG(parseStack(t, src), libs),
		Libraries: libs,
		Inputs:    map[string]any{"configs": map[string]any{"alpha": int64(1), "beta": int64(2)}},
		Store:     store,
		Factory:   stack,
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionNoOp, stepFor(plan, "resource.core.thing.many['alpha']").Decision)
	require.Equal(t, DecisionNoOp, stepFor(plan, "resource.core.thing.many['beta']").Decision)

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = exec.ApplyPlan(context.Background(), pf)
	require.NoError(t, err)
	require.Equal(t, int64(0), atomic.LoadInt64(&c.updates), "for-each instances are no-ops")
}

func TestPlanRecordsUnresolvedFieldRefs(t *testing.T) {
	src := `
resources: {
  core.thing.one: { name: 'alpha', size: 1 }
  core.thing.two: { name: resource.core.thing.one.id, size: 2 }
}
`
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	two := stepFor(plan, "resource.core.thing.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.Equal(t, []string{"resource.core.thing.one.id"}, two.UnresolvedInputs["name"])
	require.NotContains(t, two.UnresolvedInputs, "size",
		"resolved fields should not appear in UnresolvedInputs")
	require.Equal(t, PendingValue{Refs: []string{"resource.core.thing.one.id"}}, two.Inputs["name"],
		"an unresolved field keeps a placeholder the renderer shows in place")
	require.Equal(t, int64(2), two.Inputs["size"])
}

func TestPartialValueKeepsListStructure(t *testing.T) {
	expr := parseValue(t, "['lit', resource.core.thing.one.id]")
	got := partialValue(expr, &EvalContext{}, nil)
	require.Equal(t, []any{
		"lit",
		PendingValue{Refs: []string{"resource.core.thing.one.id"}},
	}, got)
}

func TestPartialValueKeepsObjectStructure(t *testing.T) {
	expr := parseValue(t, "{ ready: true, id: resource.core.thing.one.id }")
	got := partialValue(expr, &EvalContext{}, nil)
	require.Equal(t, map[string]any{
		"ready": true,
		"id":    PendingValue{Refs: []string{"resource.core.thing.one.id"}},
	}, got)
}

func TestPlanResolvesInputRefAtPlanTime(t *testing.T) {
	src := `
resources: {
  core.thing.one: { name: 'alpha', size: 1 }
  core.thing.two: { name: resource.core.thing.one.name, size: 2 }
}
`
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	two := stepFor(plan, "resource.core.thing.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.NotContains(t, two.UnresolvedInputs, "name",
		"a reference to an upstream input is known at plan, not deferred to apply")
	require.Equal(t, "alpha", two.Inputs["name"])
}

func TestPlanDoesNotResolveAPendingInput(t *testing.T) {
	src := `
resources: {
  core.thing.one:   { name: 'alpha', size: 1 }
  core.thing.two:   { name: resource.core.thing.one.id, size: 2 }
  core.thing.three: { name: resource.core.thing.two.name, size: 3 }
}
`
	var c resourceCounters
	plan := runPlan(t, src, resourceModules(&c), newStateStore(t))

	three := stepFor(plan, "resource.core.thing.three")
	require.NotNil(t, three)
	require.Equal(t, []string{"resource.core.thing.two.name"}, three.UnresolvedInputs["name"],
		"two.name is itself waiting on one.id, so reading it stays unknown at plan")
	require.Equal(t,
		PendingValue{Refs: []string{"resource.core.thing.two.name"}}, three.Inputs["name"])
}

func TestPlanExpandsLocalInUnresolvedRefs(t *testing.T) {
	src := `
locals: { one-id: resource.core.thing.one.id }
resources: {
  core.thing.one: { name: 'alpha', size: 1 }
  core.thing.two: { name: local.one-id, size: 2 }
}
`
	f := parseStack(t, src)
	var c resourceCounters
	libs := resourceModules(&c)
	exec := &Executor{
		DAG:       BuildDAG(f, libs),
		Libraries: libs,
		Source:    f,
		Store:     newStateStore(t),
		Factory:   state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)

	two := stepFor(plan, "resource.core.thing.two")
	require.NotNil(t, two)
	require.Equal(t, DecisionCreate, two.Decision)
	require.Equal(t, []string{"resource.core.thing.one.id"}, two.UnresolvedInputs["name"],
		"a field reading a local should show the resource the local waits on")
	require.Equal(t, PendingValue{Refs: []string{"resource.core.thing.one.id"}}, two.Inputs["name"])
}

func TestUpgradeActionRerunFollowsLocal(t *testing.T) {
	src := `
locals:    { thing-id: resource.core.thing.one.id }
resources: { core.thing.one: { name: 'a' } }
actions:   { core.command.notify: { argv: ['echo', local.thing-id] } }
`
	f := parseStack(t, src)
	dag := BuildDAG(f, nil)
	sl := newScopeLocals(f, dag.Nodes)

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
				{Address: "resource.core.thing.one", Kind: NodeResource, Decision: tc.upstream},
				{Address: "action.core.command.notify", Kind: NodeAction, Decision: DecisionSkip},
			}
			upgradeActionRerun(steps, dag, sl)
			require.Equal(t, tc.want, steps[1].Decision)
		})
	}
}

func TestPlanCreateWhenResourceIsGone(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	c.readFn = func(any) (any, error) { return nil, ErrNotFound }

	plan := runPlan(t, src, libs, store)
	step := stepFor(plan, "resource.core.thing.one")
	require.NotNil(t, step)
	require.Equal(t, DecisionCreate, step.Decision,
		"a missing resource with prior state should plan a recreate")
	require.True(t, step.Gone(), "step should report Gone")
	require.Empty(t, step.ObservedOutputs)
}

func TestPlanDestroyForOrphan(t *testing.T) {
	first := `
resources: { core.thing.keep: { name: 'a', size: 1 }, core.thing.orph: { name: 'b', size: 2 } }
`
	second := `
resources: { core.thing.keep: { name: 'a', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})

	plan := runPlan(t, second, libs, store)
	require.Equal(t, DecisionNoOp, decisionFor(plan, "resource.core.thing.keep"))
	require.Equal(t, DecisionDestroy, decisionFor(plan, "resource.core.thing.orph"))
}

func TestPlanRerunForChangedAction(t *testing.T) {
	first := `
actions: { core.echo.hi: { echo: 'one' } }
`
	second := `
actions: { core.echo.hi: { echo: 'two' } }
`
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeAction[echoAction, any](),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, first), libs), Libraries: libs, Store: store, Factory: stack,
	})

	plan := runPlan(t, second, libs, store)
	require.Equal(t, DecisionRerun, decisionFor(plan, "action.core.echo.hi"))
}

func TestPlanSkipForUnchangedAction(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'same' } }
`
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Actions: map[string]ActionRegistration{
				"echo": MakeAction[echoAction, any](),
			},
		},
	}
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	plan := runPlan(t, src, libs, store)
	require.Equal(t, DecisionSkip, decisionFor(plan, "action.core.echo.hi"))
}

func TestPlanRecordsStateRev(t *testing.T) {
	src := `description: 'x'`
	store := newStateStore(t)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG:       BuildDAG(parseStack(t, src), nil),
		Libraries: map[string]*Library{},
		Store:     store,
		Factory:   stack,
	})

	plan := runPlan(t, src, map[string]*Library{}, store)
	require.NotEmpty(t, plan.StateRev)
}

// planResourcesSrc builds a stack with n core.thing resources named
// r0..r(n-1) so the parallel-read tests can dial the fan-out.
func planResourcesSrc(n int) string {
	var src strings.Builder
	src.WriteString("resources: {\n")
	for i := range n {
		src.WriteString(fmt.Sprintf("  core.thing.r%d: { name: 'r%d', size: %d }\n", i, i, i))
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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	const delay = 150 * time.Millisecond
	c.readFn = func(prior any) (any, error) {
		time.Sleep(delay)
		return prior, nil
	}

	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), libs),
		Libraries:   libs,
		Store:       store,
		Factory:     stack,
		Parallelism: n,
	}
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
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	const delay = 80 * time.Millisecond
	c.readFn = func(prior any) (any, error) {
		time.Sleep(delay)
		return prior, nil
	}

	exec := &Executor{
		DAG:         BuildDAG(parseStack(t, src), libs),
		Libraries:   libs,
		Store:       store,
		Factory:     stack,
		Parallelism: 1,
	}
	start := time.Now()
	_, err := exec.Plan(context.Background())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.GreaterOrEqual(t, elapsed, time.Duration(n)*delay,
		"serial plan took %s; expected at least %s", elapsed, time.Duration(n)*delay)
}

func TestPlanPropagatesReadError(t *testing.T) {
	src := `
resources: { core.thing.one: { name: 'alpha', size: 1 } }
`
	var c resourceCounters
	store := newStateStore(t)
	libs := resourceModules(&c)
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	applyOnce(t, &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	})

	wantErr := errors.New("cloud is unwell")
	c.readFn = func(any) (any, error) { return nil, wantErr }

	exec := &Executor{
		DAG: BuildDAG(parseStack(t, src), libs), Libraries: libs, Store: store, Factory: stack,
	}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, wantErr)
	require.Contains(t, err.Error(), "resource.core.thing.one")
}
