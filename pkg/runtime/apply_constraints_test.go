package runtime

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyConstraintItem struct {
	Name         string
	GenerateName *string
	Labels       map[string]string
	Topologies   []string
}

type applyConstraintSeed struct {
	Echo  string
	Count int64
	runs  *int64
}

func (a *applyConstraintSeed) Run(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(a.runs, 1)
	return map[string]any{"value": a.Echo, "size": a.Count}, nil
}

type applyConstraintData struct {
	Key      string
	Items    []applyConstraintItem
	Limit    int64
	Metadata applyConstraintItem
	reads    *int64
}

func (d *applyConstraintData) Read(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(d.reads, 1)
	return map[string]any{"value": d.Key}, nil
}

func applyConstraintFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/apply-constraints", name)
}

func applyConstraintLibrary(
	c *resourceCounters, seedRuns, actionRuns, dataReads *int64,
) map[string]*Library {
	definition := countingResourceDefinition()
	definition.Replace.Fields = []AnyInputField[countingResource]{
		InputField(func(input *countingResource) *int64 { return &input.Size }),
	}
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[countingResource, *countingResourceOutput, any](
					definition,
					func() *countingResource { return &countingResource{counters: c} },
				),
			},
			Actions: map[string]ActionRegistration{
				"seed": MakeActionWith[applyConstraintSeed, any, any](
					func() *applyConstraintSeed { return &applyConstraintSeed{runs: seedRuns} },
				),
				"consume": MakeActionWith[countedAction, any, any](
					func() *countedAction { return &countedAction{runs: actionRuns} },
				),
			},
			DataSources: map[string]DataSourceRegistration{
				"lookup": MakeDataSourceWith[applyConstraintData, any, any](
					func() *applyConstraintData { return &applyConstraintData{reads: dataReads} },
				),
			},
			Constraints: map[string][]lang.ConstraintSpec{
				"resource.thing":     {predSpec("true", "input.name == 'good'")},
				"action.consume":     {predSpec("true", "input.echo == 'good'")},
				"data-source.lookup": {predSpec("true", "input.key == 'good'")},
			},
		},
	}
}

func savedConstraintPlan(t *testing.T, exec *Executor) *PlanFile {
	t.Helper()
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	pf, err := DecodePlan(encoded)
	require.NoError(t, err)
	return pf
}

func TestApplyDeferredInputConstraints(t *testing.T) {
	for _, kind := range []string{"resource", "data-source", "action"} {
		for _, valid := range []bool{false, true} {
			name := kind + "/invalid"
			value := "bad"
			if valid {
				name = kind + "/valid"
				value = "good"
			}
			t.Run(name, func(t *testing.T) {
				c := &resourceCounters{}
				var seedRuns, actionRuns, dataReads int64
				libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
				exec := planTestExecutor(t, applyConstraintFixture(t, "deferred-"+kind),
					libs, newStateStore(t), state.FactoryInfo{})
				exec.Inputs = map[string]any{"value": value, "size": int64(1)}
				pf := savedConstraintPlan(t, exec)
				require.Empty(t, seedRuns)
				require.Empty(t, dataReads)
				_, err := exec.ApplyPlan(context.Background(), pf)
				if valid {
					require.NoError(t, err)
					calls := map[string]int64{
						"resource": c.creates, "data-source": dataReads, "action": actionRuns,
					}
					require.EqualValues(t, 1, calls[kind])
				} else {
					require.ErrorContains(t, err,
						kind+".consumer: schema: constraints[0] (predicate)")
					require.Equal(t, 1, strings.Count(err.Error(), kind+".consumer"))
					require.Zero(t, c.creates)
					require.Zero(t, actionRuns)
					require.Zero(t, dataReads)
				}
				require.EqualValues(t, 1, seedRuns)
			})
		}
	}
}

func TestApplyDeferredResourceConstraintsBeforeMutation(t *testing.T) {
	for _, decision := range []Decision{DecisionUpdate, DecisionReplace} {
		t.Run(string(decision), func(t *testing.T) {
			c := &resourceCounters{}
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
			store := newStateStore(t)
			initial := planTestExecutor(t, applyConstraintFixture(t, "settled-resource"),
				libs, store, state.FactoryInfo{})
			initial.Inputs = map[string]any{"value": "good", "size": int64(1)}
			applyOnce(t, initial)
			exec := planTestExecutor(t, applyConstraintFixture(t, "deferred-resource"),
				libs, store, state.FactoryInfo{})
			size := int64(1)
			if decision == DecisionReplace {
				size = 2
			}
			exec.Inputs = map[string]any{"value": "bad", "size": size}
			pf := savedConstraintPlan(t, exec)
			for _, step := range pf.Steps {
				if step.Address == "resource.consumer" {
					require.Equal(t, decision, step.Decision)
				}
			}
			_, err := exec.ApplyPlan(context.Background(), pf)
			require.ErrorContains(t, err, "resource.consumer: schema: constraints[0] (predicate)")
			require.EqualValues(t, 1, c.creates)
			require.Zero(t, c.updates)
			require.Zero(t, c.deletes)
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.Equal(t, "good", snapshot.Find("resource.consumer").Inputs["name"])
		})
	}
}

func TestApplySavedNoOpInputConstraints(t *testing.T) {
	c := &resourceCounters{}
	var seedRuns, actionRuns, dataReads int64
	libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
	exec := planTestExecutor(t, applyConstraintFixture(t, "settled-resource"),
		libs, newStateStore(t), state.FactoryInfo{})
	exec.Inputs = map[string]any{"value": "good", "size": int64(1)}
	applyOnce(t, exec)
	pf := savedConstraintPlan(t, exec)
	require.Equal(t, DecisionNoOp, pf.Steps[0].Decision)
	libs["core"].Constraints["resource.thing"] = []lang.ConstraintSpec{
		predSpec("true", "input.name == 'changed-contract'"),
	}
	_, err := exec.ApplyPlan(context.Background(), pf)
	require.ErrorContains(t, err, "resource.consumer: schema: constraints[0] (predicate)")
	require.EqualValues(t, 1, c.creates)
	require.Zero(t, c.updates)
	require.Zero(t, c.deletes)
}

func TestApplyPendingReplacementInputConstraints(t *testing.T) {
	c := &resourceCounters{}
	var seedRuns, actionRuns, dataReads int64
	libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
	libs["core"].Constraints["resource.thing"] = []lang.ConstraintSpec{
		predSpec("true", "input.size == 1"),
	}
	store := newStateStore(t)
	initial := planTestExecutor(t, applyConstraintFixture(t, "settled-resource"),
		libs, store, state.FactoryInfo{})
	initial.Inputs = map[string]any{"value": "good", "size": int64(1)}
	applyOnce(t, initial)
	exec := planTestExecutor(t, applyConstraintFixture(t, "pending-replacement"),
		libs, store, state.FactoryInfo{})
	exec.Inputs = map[string]any{"size": int64(2)}
	pf := savedConstraintPlan(t, exec)
	var consumer *PlanStep
	for i := range pf.Steps {
		if pf.Steps[i].Address == "resource.consumer" {
			consumer = &pf.Steps[i]
			break
		}
	}
	require.NotNil(t, consumer)
	require.Equal(t, map[string][]string{"size": {"action.seed.size"}},
		consumer.UnresolvedInputs)
	require.Equal(t, []string{"size"}, consumer.PendingReplacementReasons)
	require.Contains(t, consumer.AllowedDecisions, DecisionReplace)
	require.Empty(t, consumer.ReplacementReasons)
	decision := consumer.Decision
	reads := c.reads
	_, err := exec.ApplyPlan(context.Background(), pf)
	require.ErrorContains(t, err, "resource.consumer: schema: constraints[0] (predicate)")
	require.Equal(t, decision, consumer.Decision)
	require.Empty(t, consumer.ReplacementReasons)
	require.Equal(t, reads, c.reads)
	require.EqualValues(t, 1, seedRuns)
	require.EqualValues(t, 1, c.creates)
	require.Zero(t, c.updates)
	require.Zero(t, c.deletes)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Find("resource.consumer").Inputs["size"])
}

func TestReplacementKeepsLargeSavedInteger(t *testing.T) {
	c := &resourceCounters{}
	var seedRuns, actionRuns, dataReads int64
	libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
	exec := planTestExecutor(t, applyConstraintFixture(t, "settled-resource"),
		libs, newStateStore(t), state.FactoryInfo{})
	const size int64 = 9007199254740993
	exec.Inputs = map[string]any{"value": "good", "size": size}
	applyOnce(t, exec)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	require.Equal(t, DecisionNoOp, plan.Steps[0].Decision)
	require.Equal(t, size, plan.Steps[0].PriorInputs["size"])
	exec.Inputs["size"] = size - 1
	plan, err = exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"size"}, plan.Steps[0].ReplacementReasons)
	require.Equal(t, DecisionReplace, plan.Steps[0].Decision)
}

func TestApplyInputConstraintsSeeDefaults(t *testing.T) {
	for _, value := range []string{"good", "bad"} {
		t.Run(value, func(t *testing.T) {
			c := &resourceCounters{}
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
			libs["core"].Defaults = map[string][]lang.DefaultSpec{
				"resource.thing": {{Field: "input.size", Value: "7"}},
			}
			libs["core"].Constraints["resource.thing"] = []lang.ConstraintSpec{
				predSpec("true", "input.name == 'good' && input.size == 7"),
			}
			store := newStateStore(t)
			exec := planTestExecutor(t, applyConstraintFixture(t, "deferred-default"),
				libs, store, state.FactoryInfo{})
			exec.Inputs = map[string]any{"value": value}
			pf := savedConstraintPlan(t, exec)
			_, err := exec.ApplyPlan(context.Background(), pf)
			if value == "bad" {
				require.ErrorContains(t, err,
					"resource.consumer: schema: constraints[0] (predicate)")
				require.Zero(t, c.creates)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, c.creates)
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.EqualValues(t, 7, snapshot.Find("resource.consumer").Inputs["size"])
		})
	}
}

func TestDestroyIgnoresHistoricalInputConstraints(t *testing.T) {
	c := &resourceCounters{}
	var seedRuns, actionRuns, dataReads int64
	libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
	exec := planTestExecutor(t, applyConstraintFixture(t, "settled-resource"),
		libs, newStateStore(t), state.FactoryInfo{})
	exec.Inputs = map[string]any{"value": "good", "size": int64(1)}
	applyOnce(t, exec)
	libs["core"].Constraints["resource.thing"] = []lang.ConstraintSpec{
		predSpec("true", "false"),
	}
	exec.Destroy = true
	applyOnce(t, exec)
	require.EqualValues(t, 1, c.deletes)
}

func TestApplyNestedForEachInputConstraints(t *testing.T) {
	for _, value := range []string{"bad", "good"} {
		t.Run(value, func(t *testing.T) {
			c := &resourceCounters{}
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
			libs["core"].Constraints["data-source.lookup"] = []lang.ConstraintSpec{{
				Kind: "predicate", When: "true", Require: "@each.value.name == 'good'",
				ForEach: "input.items", Message: "item name must be good",
			}}
			libs["w"] = &Library{
				Name: "w",
				ResourceComposites: map[string]*CompositeType{
					"box": syntaxResourceComposite(t, "box",
						applyConstraintFixture(t, "nested-body")),
				},
			}
			exec := planTestExecutor(t, applyConstraintFixture(t, "nested-call"),
				libs, newStateStore(t), state.FactoryInfo{})
			exec.Inputs = map[string]any{"value": value}
			pf := savedConstraintPlan(t, exec)
			_, err := exec.ApplyPlan(context.Background(), pf)
			if value == "good" {
				require.NoError(t, err)
				require.EqualValues(t, 1, dataReads)
			} else {
				require.ErrorContains(t, err, "resource.box['east']/data-source.consumer")
				require.ErrorContains(t, err, "item name must be good (items[1])")
				require.Zero(t, dataReads)
			}
			require.EqualValues(t, 1, seedRuns)
		})
	}
}

func TestPlanChecksKnownRulesWithDeferredInputs(t *testing.T) {
	c := &resourceCounters{}
	var seedRuns, actionRuns, dataReads int64
	libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
	libs["core"].Constraints["resource.thing"] = append(
		libs["core"].Constraints["resource.thing"], predSpec("true", "input.size > 0"),
	)
	exec := planTestExecutor(t, applyConstraintFixture(t, "deferred-resource"),
		libs, newStateStore(t), state.FactoryInfo{})
	exec.Inputs = map[string]any{"value": "bad", "size": int64(0)}
	_, err := exec.Plan(context.Background())
	require.ErrorContains(t, err, "resource.consumer: schema: constraints[1] (predicate)")
	require.NotContains(t, err.Error(), "constraints[0]")
	require.Zero(t, seedRuns)
	require.Zero(t, c.creates)
}

func TestPlanChecksKnownLabelKeysBeforeCreate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		comprehension bool
		label         string
		valid         bool
	}{
		{name: "literal", label: "Upper.example/label"},
		{name: "comprehension", comprehension: true, label: "Upper.example/label"},
		{name: "valid", comprehension: true, label: "lower.example/label", valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &resourceCounters{}
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(c, &seedRuns, &actionRuns, &dataReads)
			libs["core"].Constraints["data-source.lookup"] = []lang.ConstraintSpec{{
				Kind: "predicate", When: "true", ForEach: "input.metadata.labels ?? {}",
				Require: "@core.is-match(@each.key, '^[a-z]+[.]example/label$') || @each.key == 'good'",
				Message: "metadata.labels keys must be Kubernetes qualified names",
			}}
			exec := planTestExecutor(t, applyConstraintFixture(t, "partial-labels"),
				libs, newStateStore(t), state.FactoryInfo{})
			exec.Inputs = map[string]any{"label": tc.label, "comprehension": tc.comprehension}
			plan, err := exec.Plan(context.Background())
			require.Zero(t, c.creates)
			require.Zero(t, dataReads)
			if tc.valid {
				require.NoError(t, err)
				step := findStep(t, plan, "data-source.probe")
				require.Equal(t, []string{"resource.seed.id"}, step.UnresolvedInputs["metadata"])
				pf := savedConstraintPlan(t, exec)
				_, err := exec.ApplyPlan(context.Background(), pf)
				require.NoError(t, err)
				require.EqualValues(t, 1, c.creates)
				require.EqualValues(t, 1, dataReads)
				return
			}
			require.ErrorContains(t, err, "data-source.probe: schema: constraints[0] (predicate): "+
				"metadata.labels keys must be Kubernetes qualified names "+
				"(metadata.labels['Upper.example/label'])")
		})
	}
}

func TestPlanChecksPartiallyKnownConstraintInputs(t *testing.T) {
	rules, err := lang.ParseSource("partial-rules.ub",
		[]byte(applyConstraintFixture(t, "partial-rules")))
	require.NoError(t, err)
	expr := make(map[string]string)
	for _, field := range rules.Body.Fields {
		formatted, err := lang.FormatExpr(field.Value)
		require.NoError(t, err)
		expr[field.Key.Name] = string(formatted)
	}
	length := predSpec(expr["has-topologies"], expr["topology-limit"])
	length.ForEach = expr["items"]
	for _, tc := range []struct {
		name         string
		generateName any
		limit        int64
		owner        string
		planError    string
		applyError   string
	}{
		{
			name: "known names", generateName: "prefix-", limit: 2, owner: "bad",
			planError: "constraints[0] (exactly-one-of [items[0].name, items[0].generate-name])",
		},
		{
			name: "known length", limit: 1, owner: "bad",
			planError: "constraints[1] (predicate): predicate requirement not satisfied (items[0])",
		},
		{
			name: "pending violation", limit: 2, owner: "bad",
			applyError: "constraints[2] (predicate): predicate requirement not satisfied",
		},
		{name: "valid", limit: 2, owner: "good"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(&resourceCounters{}, &seedRuns, &actionRuns,
				&dataReads)
			libs["core"].Constraints["data-source.lookup"] = []lang.ConstraintSpec{
				setSpec("exactly-one-of", "input.items[*].name", "input.items[*].generate-name"),
				length,
				predSpec("true", expr["owner"]),
			}
			exec := planTestExecutor(t, applyConstraintFixture(t, "partial-inputs"),
				libs, newStateStore(t), state.FactoryInfo{})
			exec.Inputs = map[string]any{
				"generate-name": tc.generateName, "limit": tc.limit, "owner": tc.owner,
			}
			if tc.planError != "" {
				_, err := exec.Plan(context.Background())
				require.ErrorContains(t, err, "data-source.consumer: schema: "+tc.planError)
				require.Equal(t, 1, strings.Count(err.Error(), "constraints["))
				require.Zero(t, seedRuns)
				require.Zero(t, dataReads)
				return
			}
			pf := savedConstraintPlan(t, exec)
			require.Zero(t, dataReads)
			_, err := exec.ApplyPlan(context.Background(), pf)
			require.EqualValues(t, 1, seedRuns)
			if tc.applyError != "" {
				require.ErrorContains(t, err, "data-source.consumer: schema: "+tc.applyError)
				require.Zero(t, dataReads)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, dataReads)
			}
		})
	}
}

func TestPlanChecksListLimitThroughComposite(t *testing.T) {
	rules, err := lang.ParseSource("list-limit-rules.ub",
		[]byte(applyConstraintFixture(t, "list-limit-rules")))
	require.NoError(t, err)
	limit, err := lang.FormatExpr(rules.Body.Fields[0].Value)
	require.NoError(t, err)

	for _, body := range []string{"forwarded-list-body", "comprehension-list-body"} {
		t.Run(body, func(t *testing.T) {
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(&resourceCounters{}, &seedRuns, &actionRuns, &dataReads)
			libs["core"].Constraints["data-source.lookup"] = []lang.ConstraintSpec{{
				Kind: "predicate", When: "true", Require: string(limit),
				Message: "too many items",
			}}
			libs["w"] = &Library{ResourceComposites: map[string]*CompositeType{
				"box": syntaxResourceComposite(t, "box", applyConstraintFixture(t, body)),
			}}
			exec := planTestExecutor(t, applyConstraintFixture(t, "forwarded-list-call"),
				libs, newStateStore(t), state.FactoryInfo{})
			exec.Inputs = map[string]any{"limit": int64(1)}
			_, err := exec.Plan(context.Background())
			require.ErrorContains(t, err,
				"resource.box/data-source.consumer: schema: constraints[0] (predicate): too many items")
			require.Zero(t, seedRuns)
			require.Zero(t, dataReads)
		})
	}
}

func TestCompositeListLimitWithPendingInputs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		call      string
		limit     int64
		planError bool
	}{
		{"oversized", "forwarded-list-call", 1, true},
		{"within limit", "forwarded-list-call", 2, false},
		{"unknown size", "whole-pending-list-call", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seedRuns, actionRuns, dataReads int64
			libs := applyConstraintLibrary(&resourceCounters{}, &seedRuns, &actionRuns, &dataReads)
			body := applyConstraintFixture(t, "composite-limit-body")
			libs["w"] = &Library{ResourceComposites: map[string]*CompositeType{
				"box": syntaxResourceComposite(t, "box", body),
			}}
			exec := planTestExecutor(t, applyConstraintFixture(t, tc.call),
				libs, newStateStore(t), state.FactoryInfo{})
			exec.Inputs = map[string]any{"limit": tc.limit}
			const violation = "resource.box: schema: constraints[0] (predicate): too many items"
			if tc.planError {
				_, err := exec.Plan(context.Background())
				require.ErrorContains(t, err, violation)
				require.Zero(t, seedRuns)
				require.Zero(t, dataReads)
				return
			}
			plan := savedConstraintPlan(t, exec)
			_, err := exec.ApplyPlan(context.Background(), plan)
			require.EqualValues(t, 1, seedRuns)
			if tc.limit == 2 {
				require.NoError(t, err)
				require.EqualValues(t, 1, dataReads)
			} else {
				require.ErrorContains(t, err, violation)
				require.Zero(t, dataReads)
			}
		})
	}
}

func TestConstraintEvaluatorKeepsKnownListSizes(t *testing.T) {
	src := applyConstraintFixture(t, "list-limit-rules")
	rules, err := lang.ParseSource("list-limit-rules.ub", []byte(src))
	require.NoError(t, err)
	pending := PendingValue{Refs: []string{"action.seed.value"}}
	eval := ConstraintEvaluator(EvalContext{Inputs: map[string]any{
		"limit": int64(1),
		"items": []any{map[string]any{"name": pending}, pending},
	}})
	for _, rule := range rules.Body.Fields {
		t.Run(rule.Key.Name, func(t *testing.T) {
			got, err := eval(rule.Value, nil)
			if rule.Key.Name == "filtered" {
				require.ErrorIs(t, err, lang.ErrConstraintPending)
				return
			}
			require.NoError(t, err)
			require.Equal(t, false, got)
		})
	}
}
