package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkenc "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// freshKeyB64 returns a random 32 byte AES key encoded in base64.
func freshKeyB64(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	_, err := rand.Read(k)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(k)
}

// isJSON reports whether b parses as a JSON value.
func isJSON(b []byte) bool {
	var v any
	return json.Unmarshal(b, &v) == nil
}

// echoAction is a tiny test action: takes an Echo string, returns it
// in its outputs.
type echoAction struct {
	Echo string
}

func (a *echoAction) Run(_ context.Context, _ any) (any, error) {
	return map[string]any{"echo": a.Echo}, nil
}

type runnerAWSConfig struct {
	Region  *cfg.String
	Profile *cfg.String
}

func runnerConfigLibrary() *runtime.Library {
	return &runtime.Library{
		Name: "aws",
		Configuration: &cfg.ConfigurationType[*runnerAWSConfig]{
			New: func() *runnerAWSConfig {
				return &runnerAWSConfig{
					Region:  &cfg.String{Default: "us-east-1"},
					Profile: &cfg.String{Default: "default"},
				}
			},
		},
	}
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
	if strings.HasPrefix(strings.TrimSpace(body), "factory:") {
		return body
	}
	return "factory: {\n" + body + "\n}\n"
}

func runRoot(t *testing.T, info Info, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(info)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// applyVia runs `plan -o <tmp> -c cfg` then `apply <tmp>` and returns the
// apply output. Tests use this when they don't need to inspect the plan
// separately. An empty configPath gets a generated stack file with just the
// required state block. The plan call passes --allow-version-mismatch
// since most tests do not exercise pin verification.
func applyVia(t *testing.T, info Info, configPath string) string {
	t.Helper()
	if configPath == "" {
		configPath = writeStateStack(t, "")
	}
	planFile := filepath.Join(t.TempDir(), "plan.json")
	args := []string{"plan", "--allow-version-mismatch", "-o", planFile, "-c", configPath}
	_, err := runRoot(t, info, args...)
	require.NoError(t, err)
	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	return out
}

// stateStackBody is the state: and encryption: blocks every test stack file
// needs, now that a backend must be configured explicitly.
const stateStackBody = `state: local {
  path: '.unobin/state'
}

encryption: noop {}
`

// writeStateStack writes a stack file with the required state block plus body
// and returns its path. The file is named default.ub so its stack
// name matches the "default" a missing -c used to produce, which the state
// tests' hand-built stores also use.
func writeStateStack(t *testing.T, body string) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "default.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(stateStackBody+body)), 0o644))
	return cfg
}

// runWithStack runs a factory command with a fresh -c stack file appended, for
// commands that resolve a backend (plan, output, refresh, state). Every
// stack file basename is default.ub, so each command in a test maps to the same
// stack and shares state.
func runWithStack(t *testing.T, info Info, args ...string) (string, error) {
	t.Helper()
	return runRoot(t, info, append(args, "-c", writeStateStack(t, ""))...)
}

const backendStackBody = `state: local {
  path: '.unobin/state'
}

encryption: env-key {
  env-var: 'UB_STATE_KEY'
}
`

func writeBackendConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "default.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(backendStackBody)), 0o644))
	return cfg
}

// runBackend runs a command with a fresh -c backend-only config appended.
func runBackend(t *testing.T, info Info, args ...string) (string, error) {
	t.Helper()
	return runRoot(t, info, append(args, "-c", writeBackendConfig(t))...)
}

// openPlanFile reads a plan file from disk and returns its inner
// PlanFile, resolving the envelope's encrypter ref the same way apply does.
func openPlanFile(t *testing.T, path string) *runtime.PlanFile {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	pf, err := runtime.OpenPlan(body, func(ref *runtime.StateRef) (sdkenc.Encrypter, error) {
		return resolveEncrypter(fromRuntimeStateRef(ref))
	})
	require.NoError(t, err)
	return pf
}

func TestParseFactoryAcceptsCompilerFactoryBody(t *testing.T) {
	_, body, err := compile.ParseFactorySyntaxSource("factory.ub", []byte(`factory: {
  imports: { std: 'github.com/example/std' }
  resources: {
    hello: std.fs-file { path: '/tmp/hello' }
  }
}
`))
	require.NoError(t, err)

	_, err = parseFactory(Info{FactoryBody: body})
	require.NoError(t, err)
}

func TestParseFactoryRequiresFactoryDeclaration(t *testing.T) {
	_, err := parseFactory(Info{FactoryBody: `description: 'x'`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory.ub must declare factory")
}

func TestPlanParseError(t *testing.T) {
	info := testInfo(t, `not valid syntax {{`)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
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

func TestPlanChecksPredicateCallingFunction(t *testing.T) {
	src := `
inputs: {
  replicas: {
    type: optional(list(object({ port: optional(integer) })))
    default: [{ port: 443 }, { port: 0 }]
  }
}
imports: {
  core: 'github.com/cloudboss/unobin//pkg/libraries/core'
}
constraints: [
  {
    kind:    predicate
    when:    var.replicas != null
    require: core.all([for r in var.replicas: r.port != null && r.port > 0])
    message: 'every replica needs a positive port'
  },
]
`
	info := testInfo(t, src)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "every replica needs a positive port")
}

func TestPrintPlanShowsStateMoves(t *testing.T) {
	plan := &runtime.Plan{
		StateMoves: []runtime.PlannedEntryMove{
			{From: "core.thing@resource.old", To: "core.thing@resource.new"},
			{From: "core.thing@resource.a", To: "core.thing@resource.c"},
		},
	}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)

	require.Equal(t, `State moves:
  core.thing@resource.old -> core.thing@resource.new
  core.thing@resource.a -> core.thing@resource.c

No resource changes.
`, buf.String())
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

func TestValidateRejectsBadSource(t *testing.T) {
	info := testInfo(t, `not valid syntax {{`)
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch")
	require.Error(t, err)
}

func TestValidateRejectsInvalidReference(t *testing.T) {
	info := testInfo(t, `
actions: { bad: core.echo { echo: var.missing } }
`)
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown input "missing"`)
}

func TestStateGCKeepsLatestPlusCurrent(t *testing.T) {
	info := testInfo(t, `actions: { hi: core.echo { echo: 'hello' } }`)
	_ = applyVia(t, info, "")

	store, err := local.NewStore(
		".unobin/state", info.FactoryName, "default", encrypters.Noop{})
	require.NoError(t, err)
	currentRev, err := store.CurrentRev()
	require.NoError(t, err)

	stackInfo := state.FactoryInfo{
		Name: info.FactoryName, Version: info.FactoryVersion, ContentRevision: info.ContentRevision,
	}
	for range 4 {
		_, err := store.Write(state.NewSnapshot(stackInfo, "default"))
		require.NoError(t, err)
	}
	revs, err := store.List()
	require.NoError(t, err)
	require.Len(t, revs, 5)

	out, err := runWithStack(t, info, "state", "snapshots", "gc", "--keep", "2")
	require.NoError(t, err)
	require.Contains(t, out, "Deleted 2 snapshot(s), kept 3.")

	after, err := store.List()
	require.NoError(t, err)
	require.Equal(t, []string{currentRev, revs[3], revs[4]}, after)
}

func TestRefreshNoStateIsOK(t *testing.T) {
	info := testInfo(t, `actions: { hi: core.echo { echo: 'hello' } }`)
	out, err := runWithStack(t, info, "refresh", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "Refreshed 0, dropped 0.")
}

func TestRefreshCarriesActionsForward(t *testing.T) {
	info := testInfo(t, `
actions: { hi: core.echo { echo: 'hello' } }
outputs: { said: { value: action.hi.echo } }
`)
	_ = applyVia(t, info, "")

	out, err := runWithStack(t, info, "refresh", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "Refreshed 0, dropped 0.")

	show, err := runWithStack(t, info, "state", "list")
	require.NoError(t, err)
	require.Contains(t, show, "core.echo@action.hi")
}

func TestStateEncryptedWithEnvKey(t *testing.T) {
	src := `
actions: { hi: core.echo { echo: 'hello' } }
outputs: { said: { value: action.hi.echo } }
`
	info := testInfo(t, src)
	t.Setenv("UB_STATE_KEY", freshKeyB64(t))

	_ = applyVia(t, info, writeBackendConfig(t))

	snapDir := filepath.Join(".unobin", "state", "test-stack", "default", "snapshots")
	entries, err := os.ReadDir(snapDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	enc, err := encrypters.NewEnvKey("UB_STATE_KEY")
	require.NoError(t, err)
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(snapDir, e.Name()))
		require.NoError(t, err)
		var env state.Envelope
		require.NoError(t, json.Unmarshal(body, &env), "snapshot %s should be an envelope", e.Name())
		require.Equal(t, state.EnvelopeVersion, env.EnvelopeVersion)
		require.NotNil(t, env.Encrypter,
			"snapshot %s should record the key source that sealed it", e.Name())
		require.Equal(t, "env-key", env.Encrypter.Name)
		require.Equal(t, "UB_STATE_KEY", env.Encrypter.Body["env-var"])
		plaintext, err := enc.Decrypt(env.Ciphertext)
		require.NoError(t, err, "snapshot %s should decrypt with the configured key", e.Name())
		require.True(t, isJSON(plaintext), "decrypted snapshot %s should be JSON", e.Name())
	}

	showOut, err := runBackend(t, info, "state", "show", "core.echo@action.hi")
	require.NoError(t, err)
	require.Contains(t, showOut, `  echo: 'hello'`)
}

func TestLoadEncrypterRejectsBadKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "not-base64!!")
	_, err := loadEncrypter(nil, "")
	require.Error(t, err)
}

func TestPlanFileEncryptedWithEnvKey(t *testing.T) {
	src := `
actions: { hi: core.echo { echo: 'hello world' } }
outputs: { said: { value: action.hi.echo } }
`
	info := testInfo(t, src)
	t.Setenv("UB_STATE_KEY", freshKeyB64(t))

	planFile := filepath.Join(t.TempDir(), "plan.enc")
	_, err := runBackend(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	body, err := os.ReadFile(planFile)
	require.NoError(t, err)
	var env state.Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, state.EnvelopeVersion, env.EnvelopeVersion)
	require.NotNil(t, env.Encrypter,
		"a plan sealed by the default chain should still record its encrypter")
	require.Equal(t, "env-key", env.Encrypter.Name)
	require.Equal(t, "UB_STATE_KEY", env.Encrypter.Body["env-var"])
	require.NotEmpty(t, env.Ciphertext)
	require.False(t, isJSON(env.Ciphertext),
		"ciphertext should not parse as JSON when an encrypter is in use")

	enc, err := encrypters.NewEnvKey("UB_STATE_KEY")
	require.NoError(t, err)
	plaintext, err := enc.Decrypt(env.Ciphertext)
	require.NoError(t, err)
	require.Contains(t, string(plaintext), `"format-version": 1`)
	require.Contains(t, string(plaintext), "action.hi")

	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	require.Contains(t, out, "said: 'hello world'")
}

func TestPlanFlagOverridesConfigParallelism(t *testing.T) {
	src := `actions: { hi: core.echo { echo: 'hi' } }`
	info := testInfo(t, src)
	cfg := writeStateStack(t, `
parallelism: 3
factory: { inputs: {} }
`)
	planFile := filepath.Join(t.TempDir(), "plan.json")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch",
		"-c", cfg, "-o", planFile, "--parallelism", "7")
	require.NoError(t, err)

	pf := openPlanFile(t, planFile)
	require.Equal(t, 7, pf.Parallelism)
}

func TestPlanFallsBackToConfigParallelism(t *testing.T) {
	src := `actions: { hi: core.echo { echo: 'hi' } }`
	info := testInfo(t, src)
	cfg := writeStateStack(t, `
parallelism: 4
factory: { inputs: {} }
`)
	planFile := filepath.Join(t.TempDir(), "plan.json")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch",
		"-c", cfg, "-o", planFile)
	require.NoError(t, err)

	pf := openPlanFile(t, planFile)
	require.Equal(t, 4, pf.Parallelism)
}

func TestApplyTamperedPlanFile(t *testing.T) {
	src := `actions: { hi: core.echo { echo: 'hi' } }`
	info := testInfo(t, src)
	t.Setenv("UB_STATE_KEY", freshKeyB64(t))

	planFile := filepath.Join(t.TempDir(), "plan.enc")
	_, err := runBackend(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	body, err := os.ReadFile(planFile)
	require.NoError(t, err)
	var env state.Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmpty(t, env.Ciphertext)
	env.Ciphertext[len(env.Ciphertext)-1] ^= 0xff
	tampered, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(planFile, tampered, 0o600))

	_, err = runRoot(t, info, "apply", planFile)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
}

func TestPlanFilePlaintextWithoutEnvKey(t *testing.T) {
	src := `actions: { hi: core.echo { echo: 'hi' } }`
	info := testInfo(t, src)

	planFile := filepath.Join(t.TempDir(), "plan.json")
	_, err := runWithStack(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	body, err := os.ReadFile(planFile)
	require.NoError(t, err)
	var env state.Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, state.EnvelopeVersion, env.EnvelopeVersion)
	require.NotNil(t, env.Encrypter, "an unencrypted plan should say so explicitly")
	require.Equal(t, "noop", env.Encrypter.Name)
	require.Empty(t, env.Encrypter.Body)
	require.True(t, isJSON(env.Ciphertext),
		"with no encrypter, ciphertext should be plain plan JSON")
	require.Contains(t, string(env.Ciphertext), `"format-version": 1`)
}

// Ensure t.TempDir is visible to the loadStore call (which writes to
// `.unobin/state` relative to cwd) by chdir-ing in testInfo.
var _ = filepath.Join

func TestApplyUIServesRunView(t *testing.T) {
	t.Setenv("BROWSER", "true")
	old := uiLingerTimeout
	uiLingerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { uiLingerTimeout = old })

	info := testInfo(t, `actions: { hi: core.echo { echo: 'hello world' } }`)
	configPath := writeStateStack(t, "")
	planFile := filepath.Join(t.TempDir(), "plan.json")
	_, err := runRoot(t, info,
		"plan", "--allow-version-mismatch", "-o", planFile, "-c", configPath)
	require.NoError(t, err)

	out, err := runRoot(t, info, "apply", "--ui", planFile)
	require.NoError(t, err)
	require.Regexp(t, `Run view: http://127\.0\.0\.1:\d+/[0-9a-f]{32}/`, out)
}
