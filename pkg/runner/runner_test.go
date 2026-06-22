package runner

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// echoAction is a tiny test action: takes an Echo string, returns it
// in its outputs.
type echoAction struct {
	Echo string
}

func (a *echoAction) Run(_ context.Context, _ any) (any, error) {
	return map[string]any{"echo": a.Echo}, nil
}

func testInfo(t *testing.T, src string) Info {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(t.TempDir()))

	coreMod := &runtime.Library{
		Name: "core",
		Actions: map[string]runtime.ActionRegistration{
			"echo": runtime.MakeAction[echoAction, any, any](),
		},
		// A library-exported function, so tests can cover calls against
		// an imported library's own function set, distinct from @core.
		Functions: map[string]runtime.FunctionType{
			"all": runtime.MakeFunc("all",
				"Report whether every element of a list of booleans is true.",
				func(bools []bool) (bool, error) {
					for _, b := range bools {
						if !b {
							return false, nil
						}
					}
					return true, nil
				}),
		},
	}
	return Info{
		FactoryName:     "test-stack",
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		FactoryBody:     sourceFactory(src),
		Libraries:       map[string]*runtime.Library{"core": coreMod},
	}
}

func sourceFactory(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "factory"+":") {
		return body
	}
	return "factory" + ": {\n" + body + "\n}\n"
}

func TestParseFactoryRequiresFactoryDeclaration(t *testing.T) {
	_, err := parseFactory(Info{FactoryBody: `description: 'x'`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory.ub must declare factory")
}

func TestDeploymentID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "default"},
		{"prod.ub", "prod"},
		{"staging.ub", "staging"},
		{"prod-east.ub", "prod-east"},
		{"./prod.ub", "prod"},
		{"/tmp/foo/prod.ub", "prod"},
		{"noext", "noext"},
		{"prod.foo.ub", "prod.foo"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			require.Equal(t, c.want, stackName(c.in))
		})
	}
}

func TestParseEnvValueJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want any
	}{
		{"json object", `{"host": "web", "port": 8080}`,
			map[string]any{"host": "web", "port": int64(8080)}},
		{"json array", `["a", "b"]`, []any{"a", "b"}},
		{"json array of integers", `[1, 2, 3]`,
			[]any{int64(1), int64(2), int64(3)}},
		{"nested integers stay integers", `{"a": {"b": 1}, "c": [2, 3]}`,
			map[string]any{
				"a": map[string]any{"b": int64(1)},
				"c": []any{int64(2), int64(3)},
			}},
		{"fractional json number is a number", `{"r": 1.5}`,
			map[string]any{"r": 1.5}},
		{"json number with an exponent is a number", `{"e": 2e3}`,
			map[string]any{"e": 2000.0}},
		{"json integer at the int64 ceiling", `{"big": 9223372036854775807}`,
			map[string]any{"big": int64(9223372036854775807)}},
		{"json bool inside an object", `{"on": true}`,
			map[string]any{"on": true}},
		{"json null inside an object", `{"x": null}`,
			map[string]any{"x": nil}},
		{"ub scalar literal", `42`, int64(42)},
		{"ub list literal", `['x', 'y']`, []any{"x", "y"}},
		{"ub boolean", `true`, true},
		{"bareword falls through to the raw string", `web-prod`, "web-prod"},
		{"path falls through to the raw string", `/etc/hosts`, "/etc/hosts"},
		{"malformed json falls through to the raw string", `{not json`, "{not json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, parseEnvValue(c.raw))
		})
	}
}

func TestPrintPlanQuotesNonIdentMapKeys(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:  "resource.x",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs: map[string]any{
					"tags": map[string]any{
						"clean":       "yes",
						"has space":   "true",
						"with.dots":   "x",
						"with-dashes": "ok",
					},
				},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out,
		`tags: {clean: 'yes', 'has space': 'true', with-dashes: 'ok', 'with.dots': 'x'}`,
		"map keys that are not bare kebab identifiers must be quoted")
}

func TestPrintPlanMarksAlreadyAbsentDestroy(t *testing.T) {
	plan := &runtime.Plan{
		Destroy: true,
		Steps: []*runtime.PlanStep{
			{
				Address:     "resource.local.file.gone",
				Kind:        runtime.NodeResource,
				Decision:    runtime.DecisionDestroy,
				AlreadyGone: true,
			},
			{
				Address:  "resource.local.file.here",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionDestroy,
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, "- resource.local.file.gone  (already absent)")
	require.Contains(t, out, "- resource.local.file.here\n")
	require.NotContains(t, out, "resource.local.file.here  (already absent)")
}

func TestPrintPlanShowsUnresolvedInputRefs(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:  "resource.core.thing.two",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs: map[string]any{
					"name": nil,
					"size": int64(2),
				},
				UnresolvedInputs: map[string][]string{
					"name": {"resource.core.thing.one.name"},
				},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, "name: <resource.core.thing.one.name>")
	require.Contains(t, out, "size: 2")
}

func TestPrintPlanBracketsUnresolvedList(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:  "action.std.exec-command.run",
				Kind:     runtime.NodeAction,
				Decision: runtime.DecisionRerun,
				Inputs: map[string]any{
					"argv": []any{
						runtime.PendingValue{Refs: []string{"resource.std.fs-file.many[...].path"}},
						runtime.PendingValue{Refs: []string{"resource.std.fs-file.many[...].sha256"}},
					},
				},
				UnresolvedInputs: map[string][]string{
					"argv": {
						"resource.std.fs-file.many[...].path",
						"resource.std.fs-file.many[...].sha256",
					},
				},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out,
		`argv: [<resource.std.fs-file.many[...].path>, <resource.std.fs-file.many[...].sha256>]`)
}

func TestPrintPlanBracketsPartiallyKnownList(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:  "action.std.exec-command.run",
				Kind:     runtime.NodeAction,
				Decision: runtime.DecisionRerun,
				Inputs: map[string]any{
					"argv": []any{
						"echo",
						runtime.PendingValue{Refs: []string{"resource.std.fs-file.one.path"}},
					},
				},
				UnresolvedInputs: map[string][]string{
					"argv": {"resource.std.fs-file.one.path"},
				},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, `argv: ['echo', <resource.std.fs-file.one.path>]`)
}

func TestPrintPlanShowsInputDiffForUpdate(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:     "resource.aws.instance.web",
				Kind:        runtime.NodeResource,
				Decision:    runtime.DecisionUpdate,
				Inputs:      map[string]any{"instance-type": "t2.small", "ami": "ami-1"},
				PriorInputs: map[string]any{"instance-type": "t2.micro", "ami": "ami-1"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, `instance-type: 't2.micro' -> 't2.small'`)
	require.Contains(t, out, `ami: 'ami-1'`)
	require.NotContains(t, out, `ami: 'ami-1' -> 'ami-1'`,
		"an unchanged field should not show an arrow")
}

func TestPrintPlanTagsReplaceTrigger(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:         "resource.aws.instance.api",
				Kind:            runtime.NodeResource,
				Decision:        runtime.DecisionReplace,
				Inputs:          map[string]any{"ami": "ami-2", "instance-type": "t2.micro"},
				PriorInputs:     map[string]any{"ami": "ami-1", "instance-type": "t2.micro"},
				ReplaceTriggers: []string{"ami"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, `ami: 'ami-1' -> 'ami-2'  (forces replacement)`)
	require.NotContains(t, out, `instance-type: 't2.micro'  (forces replacement)`,
		"only the replace-forcing field is tagged")
}

func TestPrintPlanShowsDriftSection(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:         "resource.x",
				Kind:            runtime.NodeResource,
				Decision:        runtime.DecisionUpdate,
				Inputs:          map[string]any{"path": "/tmp/x"},
				PriorOutputs:    map[string]any{"path": "/tmp/x", "sha256": "old"},
				ObservedOutputs: map[string]any{"path": "/tmp/x", "sha256": "new"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, "Drift detected (1)")
	require.Contains(t, out, "  ~ resource.x")
	require.Contains(t, out, `sha256: 'old' -> 'new'`)
	driftSection := strings.SplitN(out, "\n\n", 2)[0]
	require.NotContains(t, driftSection, "path: ",
		"non-drifted fields should not appear in the drift section")
	require.Contains(t, out, "Plan: 0 to create, 1 to update")
}

func TestPrintPlanMasksSensitiveInput(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:         "resource.local.secret.s",
				Kind:            runtime.NodeResource,
				Decision:        runtime.DecisionCreate,
				Inputs:          map[string]any{"password": "shh", "name": "tok"},
				SensitiveInputs: []string{"password"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, "password: ***")
	require.NotContains(t, out, "shh")
	require.Contains(t, out, `name: 'tok'`)
}

func TestPrintPlanMasksSensitiveDrift(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:          "resource.local.secret.s",
				Kind:             runtime.NodeResource,
				Decision:         runtime.DecisionUpdate,
				Inputs:           map[string]any{"name": "tok"},
				PriorOutputs:     map[string]any{"value": "old-secret"},
				ObservedOutputs:  map[string]any{"value": "new-secret"},
				SensitiveOutputs: []string{"value"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, "value: *** -> ***")
	require.NotContains(t, out, "old-secret")
	require.NotContains(t, out, "new-secret")
}

func TestPrintPlanShowsGoneSection(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:      "resource.local.file.y",
				Kind:         runtime.NodeResource,
				Decision:     runtime.DecisionCreate,
				Inputs:       map[string]any{"path": "/tmp/y"},
				PriorOutputs: map[string]any{"path": "/tmp/y", "sha256": "abc"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	out := buf.String()
	require.Contains(t, out, "Drift detected (1)")
	require.Contains(t, out, "! resource.local.file.y  (no longer present)")
	require.Contains(t, out, "Plan: 1 to create")
}

func TestPrintPlanGroupsForEachInstances(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:  "resource.core.thing.many['alpha']",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs:   map[string]any{"name": "alpha", "size": int64(1)},
			},
			{
				Address:  "resource.core.thing.many['beta']",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs:   map[string]any{"name": "beta", "size": int64(2)},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	expected := `  + resource.core.thing.many  (for-each, 2 instances)
    + ['alpha']
        name: 'alpha'
        size: 1
    + ['beta']
        name: 'beta'
        size: 2

Plan: 2 to create, 0 to update, 0 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, expected, buf.String())
}

func TestPrintPlanGroupsForEachInstancesInsideComposite(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:   "resource.greeter.greeting.welcome",
				Kind:      runtime.NodeResource,
				Composite: true,
				Decision:  runtime.DecisionEval,
				Inputs:    map[string]any{"path": "/tmp/x"},
			},
			{
				Address:  "resource.greeter.greeting.welcome/resource.local.file.many['a']",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs:   map[string]any{"path": "/tmp/a"},
			},
			{
				Address:  "resource.greeter.greeting.welcome/resource.local.file.many['b']",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs:   map[string]any{"path": "/tmp/b"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	expected := `  + resource.greeter.greeting.welcome  (composite)
      path: '/tmp/x'
    + resource.local.file.many  (for-each, 2 instances)
      + ['a']
          path: '/tmp/a'
      + ['b']
          path: '/tmp/b'

Plan: 2 to create, 0 to update, 0 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, expected, buf.String())
}

func TestPrintPlanGroupsCompositeInternals(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:   "resource.greeter.greeting.welcome",
				Kind:      runtime.NodeResource,
				Composite: true,
				Decision:  runtime.DecisionEval,
				Inputs: map[string]any{
					"message": "Hello",
					"path":    "/tmp/x",
				},
			},
			{
				Address:  "resource.greeter.greeting.welcome/resource.local.file.this",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs: map[string]any{
					"content": "Hello",
					"path":    "/tmp/x",
				},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	expected := `  + resource.greeter.greeting.welcome  (composite)
      message: 'Hello'
      path: '/tmp/x'
    + resource.local.file.this
        content: 'Hello'
        path: '/tmp/x'

Plan: 1 to create, 0 to update, 0 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, expected, buf.String())
}

func TestPrintPlanRendersNestedComposites(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:   "resource.greeter.greeting.welcome",
				Kind:      runtime.NodeResource,
				Composite: true,
				Decision:  runtime.DecisionEval,
				Inputs: map[string]any{
					"message": "Hello",
					"path":    "/tmp/x",
				},
			},
			{
				Address:   "resource.greeter.greeting.welcome/resource.helloer.hello.file",
				Kind:      runtime.NodeResource,
				Composite: true,
				Decision:  runtime.DecisionEval,
				Inputs: map[string]any{
					"message": "Hello",
					"path":    "/tmp/x",
				},
			},
			{
				Address:  "resource.greeter.greeting.welcome/resource.helloer.hello.file/resource.local.file.this",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionCreate,
				Inputs: map[string]any{
					"content": "Hello",
					"path":    "/tmp/x",
				},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	expected := `  + resource.greeter.greeting.welcome  (composite)
      message: 'Hello'
      path: '/tmp/x'
    + resource.helloer.hello.file  (composite)
        message: 'Hello'
        path: '/tmp/x'
      + resource.local.file.this
          content: 'Hello'
          path: '/tmp/x'

Plan: 1 to create, 0 to update, 0 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, expected, buf.String())
}

func TestAsciiLabel(t *testing.T) {
	tests := []struct {
		d    runtime.Decision
		want string
	}{
		{runtime.DecisionCreate, "(create) "},
		{runtime.DecisionUpdate, "(update) "},
		{runtime.DecisionReplace, "(replace)"},
		{runtime.DecisionDestroy, "(destroy)"},
		{runtime.DecisionRerun, "(rerun)  "},
		{runtime.DecisionSkip, "(skip)   "},
		{runtime.DecisionRead, "(read)   "},
		{runtime.DecisionNoOp, "(noop)   "},
		{runtime.DecisionEval, "(eval)   "},
	}
	for _, tt := range tests {
		t.Run(string(tt.d), func(t *testing.T) {
			got := asciiLabel(tt.d)
			require.Equal(t, tt.want, got)
			require.Len(t, got, 9, "labels pad to a common width so addresses align")
		})
	}
}

func TestPrintPlanAsciiUsesWordLabels(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{Address: "resource.aws.lb.main", Kind: runtime.NodeResource, Decision: runtime.DecisionReplace},
			{Address: "resource.aws.vpc.main", Kind: runtime.NodeResource, Decision: runtime.DecisionCreate},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, true)
	expected := `  (replace) resource.aws.lb.main
  (create)  resource.aws.vpc.main

Plan: 1 to create, 0 to update, 1 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, expected, buf.String())
}

func TestPrintPlanHidesCompositeWhenInternalsUnchanged(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:   "resource.greeter.greeting.welcome",
				Kind:      runtime.NodeResource,
				Composite: true,
				Decision:  runtime.DecisionEval,
				Inputs:    map[string]any{"message": "Hello"},
			},
			{
				Address:  "resource.greeter.greeting.welcome/resource.local.file.this",
				Kind:     runtime.NodeResource,
				Decision: runtime.DecisionNoOp,
				Inputs:   map[string]any{"content": "Hello"},
			},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	require.Equal(t, "No changes.\n", buf.String())
}

func TestRootIsCobraTree(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	root := newRootCmd(info)
	require.IsType(t, &cobra.Command{}, root)
	require.Equal(t, "test-stack", root.Use)
	subs := map[string]bool{}
	for _, c := range root.Commands() {
		subs[c.Name()] = true
	}
	require.True(t, subs["version"])
	require.True(t, subs["plan"])
	require.True(t, subs["apply"])
	require.True(t, subs["refresh"])
	require.True(t, subs["validate"])
	require.True(t, subs["output"])
	require.True(t, subs["schema"])
	require.True(t, subs["state"])
}

func TestLoadEncrypterRejectsBadKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "not-base64!!")
	_, err := loadEncrypter(nil, "")
	require.Error(t, err)
}
