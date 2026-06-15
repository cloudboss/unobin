package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
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

func testInfo(t *testing.T, src string) Info {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(t.TempDir()))

	coreMod := &runtime.Library{
		Name: "core",
		Actions: map[string]runtime.ActionRegistration{
			"echo": runtime.MakeAction[echoAction, any](),
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
		FactoryBody:     src,
		Libraries:       map[string]*runtime.Library{"core": coreMod},
	}
}

// testFileLibrary registers a minimal file-on-disk resource so the
// destroy lifecycle runs against something real: Create writes the
// file, Read reports absence, and Delete removes it.
func testFileLibrary() *runtime.Library {
	return &runtime.Library{
		Name: "local",
		Resources: map[string]runtime.ResourceRegistration{
			"file": runtime.MakeResource[fileResource, any](),
		},
	}
}

type fileResource struct {
	Path    string
	Content string
	Mode    int64
}

func (f *fileResource) Create(_ context.Context, _ any) (any, error) {
	if err := os.WriteFile(f.Path, []byte(f.Content), os.FileMode(f.Mode)); err != nil {
		return nil, err
	}
	return map[string]any{"path": f.Path}, nil
}

func (f *fileResource) Read(_ context.Context, _ any, prior any) (any, error) {
	if _, err := os.Stat(f.Path); err != nil {
		if os.IsNotExist(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, err
	}
	return prior, nil
}

func (f *fileResource) Update(
	_ context.Context, _ any, _ runtime.Prior[fileResource, any],
) (any, error) {
	return f.Create(context.Background(), nil)
}

func (f *fileResource) Delete(_ context.Context, _ any, _ any) error {
	err := os.Remove(f.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (f *fileResource) SchemaVersion() int      { return 1 }
func (f *fileResource) ReplaceFields() []string { return []string{"path"} }

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
// separately. An empty configPath gets a generated config with just the
// required state block. The plan call passes --allow-version-mismatch
// since most tests do not exercise pin verification.
func applyVia(t *testing.T, info Info, configPath string) string {
	t.Helper()
	if configPath == "" {
		configPath = writeStateConfig(t, "")
	}
	planFile := filepath.Join(t.TempDir(), "plan.json")
	args := []string{"plan", "--allow-version-mismatch", "-o", planFile, "-c", configPath}
	_, err := runRoot(t, info, args...)
	require.NoError(t, err)
	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	return out
}

// stateConfigBody is the state: and encryption: blocks every test config
// needs, now that a backend must be configured explicitly.
const stateConfigBody = `state: local {
  path: '.unobin/state'
}

encryption: noop {}
`

// writeStateConfig writes a config with the required state block plus body
// and returns its path. The file is named default.ub so its stack
// name matches the "default" a missing -c used to produce, which the state
// tests' hand-built stores also use.
func writeStateConfig(t *testing.T, body string) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "default.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(stateConfigBody+body)), 0o644))
	return cfg
}

// runCfg runs a factory command with a fresh -c state config appended, for
// commands that resolve a backend (plan, output, refresh, state). Every
// config basename is default.ub, so each command in a test maps to the same
// stack and shares state.
func runCfg(t *testing.T, info Info, args ...string) (string, error) {
	t.Helper()
	return runRoot(t, info, append(args, "-c", writeStateConfig(t, ""))...)
}

const backendConfigBody = `state: local {
  path: '.unobin/state'
}

encryption: env-key {
  env-var: 'UB_STATE_KEY'
}
`

func writeBackendConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "default.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(backendConfigBody)), 0o644))
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

func TestVersion(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	out, err := runRoot(t, info, "version")
	require.NoError(t, err)
	require.Contains(t, out, "test-stack v0.1.0 (content-revision abcdef)")
}

func TestParsedFileAcceptsCompilerFactoryBody(t *testing.T) {
	_, body, err := compile.ParseFactorySource("factory.ub", []byte(`factory: {
  imports: { std: 'github.com/example/std' }
  resources: {
    hello: std.fs-file { path: '/tmp/hello' }
  }
}
`))
	require.NoError(t, err)

	_, _, err = parsedFile(Info{FactoryBody: body})
	require.NoError(t, err)
}

func TestApplyAndOutput(t *testing.T) {
	info := testInfo(t, `
actions: { core.echo.hi: { echo: 'hello world' } }
outputs: { said: { value: action.core.echo.hi.echo } }
`)
	apply := applyVia(t, info, "")
	require.Contains(t, apply, "said: 'hello world'")

	all, err := runCfg(t, info, "output")
	require.NoError(t, err)
	require.Contains(t, all, "said: 'hello world'")

	one, err := runCfg(t, info, "output", "said")
	require.NoError(t, err)
	require.Contains(t, one, "hello world")
}

func TestPlanDestroyRemovesResources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.txt")
	src := fmt.Sprintf(`
resources: { local.file.x: { path: '%s', content: 'hello', mode: 420 } }
`, path)
	info := testInfo(t, src)
	info.Libraries["local"] = testFileLibrary()

	// Create the file with a normal apply.
	_ = applyVia(t, info, "")
	_, err := os.Stat(path)
	require.NoError(t, err)

	// A destroy plan renders the resource as a deletion and counts it.
	render, err := runCfg(t, info, "plan", "--destroy", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, render, "- resource.local.file.x")
	require.Contains(t, render, "0 to create, 0 to update, 0 to replace, 1 to destroy")

	// A destroy plan should mark the file for deletion.
	planFile := filepath.Join(t.TempDir(), "destroy.json")
	_, err = runCfg(t, info, "plan", "--destroy", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	pf := openPlanFile(t, planFile)
	require.True(t, pf.Destroy)
	require.Len(t, pf.Steps, 1)
	require.Equal(t, runtime.DecisionDestroy, pf.Steps[0].Decision)

	// Applying it removes the file and empties state.
	_, err = runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "file should be removed after destroy")

	out, err := runCfg(t, info, "state", "list")
	require.NoError(t, err)
	require.NotContains(t, out, "resource.local.file.x")
}

func TestOutputJSON(t *testing.T) {
	info := testInfo(t, `
actions: { core.echo.hi: { echo: 'hello world' } }
outputs: { said: { value: action.core.echo.hi.echo }, count: { value: 7 } }
`)
	_ = applyVia(t, info, "")

	all, err := runCfg(t, info, "output", "--json")
	require.NoError(t, err)
	require.Equal(t, "{\n  \"count\": 7,\n  \"said\": \"hello world\"\n}\n", all)

	one, err := runCfg(t, info, "output", "--json", "said")
	require.NoError(t, err)
	require.Equal(t, "\"hello world\"\n", one)
}

func TestOutputUnknownName(t *testing.T) {
	info := testInfo(t, `outputs: { x: { value: 'y' } }`)
	_ = applyVia(t, info, "")
	_, err := runCfg(t, info, "output", "missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no output")
}

func TestOutputBeforeApply(t *testing.T) {
	info := testInfo(t, `outputs: { x: { value: 'y' } }`)
	_, err := runCfg(t, info, "output")
	require.Error(t, err)
}

func TestPlanParseError(t *testing.T) {
	info := testInfo(t, `not valid syntax {{`)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
}

func TestApplyWithConfigInputs(t *testing.T) {
	src := `
inputs:  { greeting: { type: string } }
actions: { core.echo.hi: { echo: var.greeting } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)

	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(stateConfigBody+`
factory: {
  inputs: {
    greeting: 'from-config'
  }
}
`)), 0o644))

	out := applyVia(t, info, cfg)
	require.Contains(t, out, "said: 'from-config'")
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

func TestPlanIsolatesDeploymentsByConfigName(t *testing.T) {
	src := `
inputs:  { greeting: { type: string } }
actions: { core.echo.hi: { echo: var.greeting } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)

	prod := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(prod,
		[]byte(sourceStack(stateConfigBody+`factory: { inputs: { greeting: 'hello-prod' } }`)),
		0o644))
	staging := filepath.Join(filepath.Dir(prod), "staging.ub")
	require.NoError(t, os.WriteFile(staging,
		[]byte(sourceStack(stateConfigBody+`factory: { inputs: { greeting: 'hello-staging' } }`)),
		0o644))

	out := applyVia(t, info, prod)
	require.Contains(t, out, "said: 'hello-prod'")
	out = applyVia(t, info, staging)
	require.Contains(t, out, "said: 'hello-staging'")

	// Both stacks now have their own snapshot directory.
	prodSnap := filepath.Join(".unobin/state", info.FactoryName, "prod")
	stagingSnap := filepath.Join(".unobin/state", info.FactoryName, "staging")
	_, err := os.Stat(prodSnap)
	require.NoError(t, err)
	_, err = os.Stat(stagingSnap)
	require.NoError(t, err)
}

func TestEnvVarOverridesConfig(t *testing.T) {
	src := `
inputs:  { greeting: { type: string } }
actions: { core.echo.hi: { echo: var.greeting } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)

	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(stateConfigBody+`
factory: {
  inputs: {
    greeting: 'from-config'
  }
}
`)), 0o644))

	t.Setenv("UB_VAR_greeting", "from-env")
	out := applyVia(t, info, cfg)
	require.Contains(t, out, "said: 'from-env'")
}

func TestEnvVarUnderscoreToHyphen(t *testing.T) {
	src := `
inputs:  { cluster-name: { type: string } }
actions: { core.echo.hi: { echo: var.cluster-name } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)

	t.Setenv("UB_VAR_cluster_name", "web-prod")
	out := applyVia(t, info, "")
	require.Contains(t, out, "said: 'web-prod'")
}

func TestEnvVarParsesTypedLiterals(t *testing.T) {
	src := `
inputs: {
  size:     { type: integer }
  use-spot: { type: boolean }
  ratio:    { type: number }
  subnets:  { type: list(string) }
}
imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
actions: {
  core.echo.summary: {
    echo: $'''\-
      size={{ var.size }} spot={{ var.use-spot }} ratio=
      {{ var.ratio }} subnets={{ @core.join(var.subnets, ',') }}
      '''
  }
}
outputs: { said: { value: action.core.echo.summary.echo } }
`
	info := testInfo(t, src)

	t.Setenv("UB_VAR_size", "5")
	t.Setenv("UB_VAR_use_spot", "true")
	t.Setenv("UB_VAR_ratio", "1.5")
	t.Setenv("UB_VAR_subnets", "['subnet-a', 'subnet-b']")

	out := applyVia(t, info, "")
	require.Contains(t, out,
		"said: 'size=5 spot=true ratio=1.5 subnets=subnet-a,subnet-b'")
}

func TestEnvVarStringInputTakesRawValue(t *testing.T) {
	src := `
inputs:  { answer: { type: string }, comment: { type: optional(string) } }
actions: { core.echo.hi: { echo: var.answer } }
outputs: { said: { value: action.core.echo.hi.echo }, note: { value: var.comment ?? 'none' } }
`
	info := testInfo(t, src)
	// Each value parses as a UB literal, but the declared type is
	// string, so the raw text arrives untouched.
	t.Setenv("UB_VAR_answer", "true")
	t.Setenv("UB_VAR_comment", "42")
	out := applyVia(t, info, "")
	require.Contains(t, out, "said: 'true'")
	require.Contains(t, out, "note: '42'")
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

func TestEnvVarParsesJSON(t *testing.T) {
	// JSON uses double quotes for strings and keys, which UB does not,
	// so a JSON object or array is not valid UB and reaches inputs only
	// through the JSON fallback. The integer 8080 must decode as an
	// integer to satisfy the declared field; a JSON float would fail
	// input validation.
	src := `
inputs: {
  config: { type: object({ host: string, port: integer }) }
  tags:   { type: list(string) }
}
imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
actions: { core.echo.hi: { echo: 'x' } }
outputs: {
  result: {
    value: @core.to-json({ host: var.config.host, port: var.config.port, tags: var.tags })
  }
}
`
	info := testInfo(t, src)

	t.Setenv("UB_VAR_config", `{"host": "web", "port": 8080}`)
	t.Setenv("UB_VAR_tags", `["a", "b"]`)

	out := applyVia(t, info, "")
	require.Contains(t, out, `{"host":"web","port":8080,"tags":["a","b"]}`)
}

func TestPlanRejectsTypeMismatch(t *testing.T) {
	src := `
inputs: {
  size: { type: integer }
}
`
	info := testInfo(t, src)
	t.Setenv("UB_VAR_size", "'not-a-number'")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), `input "size"`)
	require.Contains(t, err.Error(), "expected integer")
}

func TestPlanRejectsMissingRequiredInput(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
}
`
	info := testInfo(t, src)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), `input "region"`)
	require.Contains(t, err.Error(), "required but not provided")
}

func TestPlanRejectsUnknownInput(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
}
`
	info := testInfo(t, src)
	t.Setenv("UB_VAR_region", "us-east-1")
	t.Setenv("UB_VAR_clustr_name", "typo")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown input "clustr-name"`)
}

func TestPlanAppliesDeclaredDefault(t *testing.T) {
	src := `
inputs:  { size: { type: integer, default: 3 } }
imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
actions: { core.echo.hi: { echo: $'size={{ var.size }}' } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)
	out := applyVia(t, info, "")
	require.Contains(t, out, "said: 'size=3'")
}

func TestPlanRejectsConstraintViolation(t *testing.T) {
	src := `
inputs: {
  vpc-id:     { type: optional(string) }
  subnet-ids: { type: optional(list(string)) }
}
constraints: [
  { kind: required-together, fields: [var.vpc-id, var.subnet-ids] },
]
`
	info := testInfo(t, src)
	t.Setenv("UB_VAR_vpc_id", "vpc-abc")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "required-together")
}

func TestPlanRejectsSplatConstraintViolation(t *testing.T) {
	src := `
inputs: {
  replicas: {
    type: list(object({ inline: optional(string), from-file: optional(string) }))
    default: [{ inline: 'a', from-file: 'f' }]
  }
}
constraints: [
  { kind: exactly-one-of, fields: [var.replicas[*].inline, var.replicas[*].from-file] },
]
`
	info := testInfo(t, src)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"expected exactly one to be set, got 2 (var.replicas[0].inline, var.replicas[0].from-file)")
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

// TestPlanChecksPredicateCallingCoreNamespace proves a constraint
// predicate can call @core with no import at all: the namespace is
// part of the language, in scope everywhere expressions evaluate.
func TestPlanChecksPredicateCallingCoreNamespace(t *testing.T) {
	src := `
inputs: {
  replicas: {
    type: optional(list(object({ port: optional(integer) })))
    default: [{ port: 443 }, { port: 0 }]
  }
}
constraints: [
  {
    kind:    predicate
    when:    var.replicas != null
    require: @core.all([for r in var.replicas: r.port != null && r.port > 0])
    message: 'every replica needs a positive port'
  },
]
`
	info := testInfo(t, src)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "every replica needs a positive port")
}

func TestPlanRejectsPredicate(t *testing.T) {
	src := `
inputs: {
  region:    { type: string }
  fips-mode: { type: boolean, default: false }
}
constraints: [
  {
    kind:    predicate
    when:    var.region == 'us-gov-east-1'
    require: var.fips-mode == true
    message: 'GovCloud regions require FIPS mode enabled'
  },
]
`
	info := testInfo(t, src)
	t.Setenv("UB_VAR_region", "us-gov-east-1")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "GovCloud regions require FIPS mode enabled")
}

func TestPlanChecksPredicateOverNestedInput(t *testing.T) {
	src := `
inputs: {
  code: { type: optional(object({ inline: optional(string) })) }
}
constraints: [
  {
    kind:    predicate
    when:    true
    require: var.code.inline != null
    message: 'code must be inline'
  },
]
`
	info := testInfo(t, src)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "code must be inline")
}

func TestPlanAllowsPredicateOverUnsetNestedInput(t *testing.T) {
	src := `
inputs: {
  code: { type: optional(object({ inline: optional(string) })) }
  size: { type: optional(integer) }
}
constraints: [
  { kind: predicate, when: var.code.inline != null, require: var.size != null },
]
`
	info := testInfo(t, src)
	configPath := writeStateConfig(t, "")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch", "-c", configPath)
	require.NoError(t, err)
}

func TestPlanRejectsValueOutsideMinimum(t *testing.T) {
	src := `
inputs: {
  size: { type: integer, minimum: 1 }
}
`
	info := testInfo(t, src)
	t.Setenv("UB_VAR_size", "0")
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "below minimum")
}

func TestEnvVarUnparseableFallsBackToString(t *testing.T) {
	// URLs, paths, and names with special characters do not parse as UB
	// literals; they arrive as plain strings without shell-escape ceremony.
	src := `
inputs:  { endpoint: { type: string } }
actions: { core.echo.hi: { echo: var.endpoint } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)
	t.Setenv("UB_VAR_endpoint", "https://example.com/health")
	out := applyVia(t, info, "")
	require.Contains(t, out, "said: 'https://example.com/health'")
}

func TestEnvVarStringInputKeepsQuoteCharacters(t *testing.T) {
	src := `
inputs:  { greeting: { type: string } }
actions: { core.echo.hi: { echo: var.greeting } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)
	// A string input takes the raw text, so UB-style quotes are data,
	// not syntax.
	t.Setenv("UB_VAR_greeting", "'hello world'")
	out := applyVia(t, info, "")
	require.Contains(t, out, `said: '\'hello world\''`)
}

func TestPlanShowsCreateBeforeApply(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
`
	info := testInfo(t, src)
	out, err := runCfg(t, info, "plan", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "↺ action.core.echo.hi")
	require.Contains(t, out, `echo: 'hello'`)
	require.Contains(t, out, "Plan: 0 to create, 0 to update, 0 to replace, 0 to destroy, 1 to rerun.")
}

func TestPlanHidesSkipAfterApply(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	out, err := runCfg(t, info, "plan", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "No changes.")
}

func TestPrintPlanQuotesNonIdentMapKeys(t *testing.T) {
	plan := &runtime.Plan{
		Steps: []*runtime.PlanStep{
			{
				Address:  "resource.local.file.x",
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
				Address:         "resource.local.file.x",
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
	require.Contains(t, out, "  ~ resource.local.file.x")
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

func TestSchemaTemplate(t *testing.T) {
	src := `
inputs: {
  greeting: {
    type:        string
    description: 'Text to write'
  }
  count:   { type: integer }
  enabled: { type: boolean }
  tags:    { type: list(string) }
}
`
	info := testInfo(t, src)
	out, err := runRoot(t, info, "schema", "template")
	require.NoError(t, err)

	expected := `stack: {
  factory: {
    pin: {
      supported-versions: [
        { version: 'v0.1.0', content-revision: 'abcdef' },
      ]
    }
    inputs: {
      # Text to write
      greeting: ''  # type: string
      count:    0  # type: integer
      enabled:  false  # type: boolean
      tags:     []  # type: list(string)
    }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: noop {}
}
`
	require.Equal(t, expected, out)
}

func TestSchemaTemplateNoInputs(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	out, err := runRoot(t, info, "schema", "template")
	require.NoError(t, err)
	expected := `stack: {
  factory: {
    pin: {
      supported-versions: [
        { version: 'v0.1.0', content-revision: 'abcdef' },
      ]
    }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: noop {}
}
`
	require.Equal(t, expected, out)
}

func TestTemplateIncludesLibraryPathWhenSet(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	info.LibraryPath = "github.com/cloudboss/cluster-deploy"
	out, err := runRoot(t, info, "schema", "template")
	require.NoError(t, err)
	expected := `stack: {
  factory: {
    pin: {
      library-path: 'github.com/cloudboss/cluster-deploy'
      supported-versions: [
        { version: 'v0.1.0', content-revision: 'abcdef' },
      ]
    }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: noop {}
}
`
	require.Equal(t, expected, out)
}

func TestSchemaTemplateWritesToFile(t *testing.T) {
	info := testInfo(t, `inputs: { greeting: { type: string } }`)
	dst := filepath.Join(t.TempDir(), "dev.ub")
	stdout, err := runRoot(t, info, "schema", "template", "-o", dst)
	require.NoError(t, err)
	require.Empty(t, stdout)

	written, err := os.ReadFile(dst)
	require.NoError(t, err)
	expected := `stack: {
  factory: {
    pin: {
      supported-versions: [
        { version: 'v0.1.0', content-revision: 'abcdef' },
      ]
    }
    inputs: {
      greeting: ''  # type: string
    }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: noop {}
}
`
	require.Equal(t, expected, string(written))
}

func TestSchemaTemplateScaffoldResolves(t *testing.T) {
	info := testInfo(t, `inputs: { greeting: { type: string } }`)
	out, err := runRoot(t, info, "schema", "template")
	require.NoError(t, err)

	path := writeConfig(t, out)
	f, err := parseConfigFile(path)
	require.NoError(t, err)
	sc, err := parseStateConfig(f, path)
	require.NoError(t, err)

	enc, err := resolveEncrypter(sc.Encrypter)
	require.NoError(t, err)
	_, isNoop := enc.(encrypters.Noop)
	require.True(t, isNoop, "scaffold should resolve to the noop encrypter, got %T", enc)

	be, err := resolveBackend(sc.Backend, info.FactoryName, "default", enc)
	require.NoError(t, err)
	require.NotNil(t, be)
}

func TestPlanEmpty(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	out, err := runCfg(t, info, "plan", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "No changes.")
}

func TestPlanWritesPlanFile(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
`
	info := testInfo(t, src)
	planFile := filepath.Join(t.TempDir(), "plan.json")

	_, err := runCfg(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	pf := openPlanFile(t, planFile)
	require.Equal(t, 1, pf.FormatVersion)
	addresses := make([]string, len(pf.Steps))
	for i, s := range pf.Steps {
		addresses[i] = s.Address
	}
	require.Contains(t, addresses, "action.core.echo.hi")
}

func TestApplyConsumesPlanFile(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello world' } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)
	planFile := filepath.Join(t.TempDir(), "plan.json")

	_, err := runCfg(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	require.Contains(t, out, "said: 'hello world'")
}

func TestPlanMissingConfigFile(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	_, err := runRoot(t, info, "plan", "-c", "/no/such/path.ub")
	require.Error(t, err)
}

func TestApplyMissingPlanFile(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	_, err := runRoot(t, info, "apply", "/no/such/plan.json")
	require.Error(t, err)
}

func TestApplyRequiresPlanFile(t *testing.T) {
	info := testInfo(t, "description: 'x'")
	_, err := runRoot(t, info, "apply")
	require.Error(t, err)
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

func TestValidateAcceptsCleanSource(t *testing.T) {
	info := testInfo(t, `
actions: { core.echo.hi: { echo: 'hello' } }
`)
	out, err := runRoot(t, info, "validate", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "OK")
}

func TestValidateRejectsBadSource(t *testing.T) {
	info := testInfo(t, `not valid syntax {{`)
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch")
	require.Error(t, err)
}

func TestValidateRejectsInvalidReference(t *testing.T) {
	info := testInfo(t, `
actions: { core.echo.bad: { echo: var.missing } }
`)
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown input "missing"`)
}

func TestValidateChecksConfig(t *testing.T) {
	info := testInfo(t, `
inputs:  { greeting: { type: string } }
actions: { core.echo.hi: { echo: var.greeting } }
`)
	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(`bogus { not valid`), 0o644))
	_, err := runRoot(t, info, "validate", "-c", cfg)
	require.Error(t, err)
}

func TestValidateRejectsUnknownBackend(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(`
state: ghost {}

encryption: noop {}
`)), 0o644))
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch", "-c", cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), `no backend named "ghost"`)
}

func TestValidateRejectsBadBackendBody(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(`
state: local { unknown-field: 1 }

encryption: noop {}
`)), 0o644))
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch", "-c", cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
}

func TestValidateRejectsUnknownEncrypter(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(`
state: local { path: '.unobin/state' }

encryption: ghost {}
`)), 0o644))
	_, err := runRoot(t, info, "validate", "--allow-version-mismatch", "-c", cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), `no key-source named "ghost"`)
}

func TestValidateAcceptsCoreBackendAndEncrypter(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	cfg := filepath.Join(t.TempDir(), "prod.ub")
	require.NoError(t, os.WriteFile(cfg, []byte(sourceStack(`
state: local { path: '.unobin/state' }

encryption: env-key { env-var: 'UB_STATE_KEY' }
`)), 0o644))
	out, err := runRoot(t, info, "validate", "--allow-version-mismatch", "-c", cfg)
	require.NoError(t, err)
	require.Contains(t, out, "OK")
}

func TestPrintGraphPlain(t *testing.T) {
	src := `
inputs: { msg: { type: string } }
actions: {
  core.echo.first:  { echo: var.msg }
  core.echo.second: { echo: action.core.echo.first.echo }
}
`
	info := testInfo(t, src)
	out, err := runRoot(t, info, "print-graph")
	require.NoError(t, err)
	expected := `action.core.echo.first
  -> var.msg

action.core.echo.second
  -> action.core.echo.first
`
	require.Equal(t, expected, out)
}

func TestPrintGraphDot(t *testing.T) {
	src := `
inputs: { msg: { type: string } }
actions: {
  core.echo.first:  { echo: var.msg }
  core.echo.second: { echo: action.core.echo.first.echo }
}
`
	info := testInfo(t, src)
	out, err := runRoot(t, info, "print-graph", "--format", "dot")
	require.NoError(t, err)
	expected := `digraph "test-stack" {
  "action.core.echo.first";
  "action.core.echo.second";
  "action.core.echo.second" -> "action.core.echo.first";
}
`
	require.Equal(t, expected, out)
}

func TestPrintGraphRejectsUnknownFormat(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	_, err := runRoot(t, info, "print-graph", "--format", "yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--format")
}

func TestStateMoveRelocatesEntry(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	out, err := runCfg(t, info, "state", "move", "action.core.echo.hi", "action.core.echo.bye")
	require.NoError(t, err)
	require.Contains(t, out, "Moved action.core.echo.hi to action.core.echo.bye")

	show, err := runCfg(t, info, "state", "show")
	require.NoError(t, err)
	require.Contains(t, show, "action.core.echo.bye")
	require.NotContains(t, show, "action.core.echo.hi ")
}

func TestStateMoveRejectsMissingSource(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hello' } }`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	_, err := runCfg(t, info, "state", "move", "action.core.echo.gone", "action.core.echo.elsewhere")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no entry at")
}

func TestStateMoveRejectsCollision(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' }, core.echo.bye: { echo: 'bye' } }
`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	_, err := runCfg(t, info, "state", "move", "action.core.echo.hi", "action.core.echo.bye")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestStateRemoveDropsEntry(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hello' } }`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	out, err := runCfg(t, info, "state", "remove", "action.core.echo.hi")
	require.NoError(t, err)
	require.Contains(t, out, "Removed action.core.echo.hi")

	show, err := runCfg(t, info, "state", "show")
	require.NoError(t, err)
	require.NotContains(t, show, "action.core.echo.hi")
}

func TestStateRemoveRejectsMissing(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hello' } }`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	_, err := runCfg(t, info, "state", "remove", "action.core.echo.gone")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no entry at")
}

// stateMoveFixture builds a snapshot that mixes a library call site
// (boundary + one internal) with one unrelated leaf so the move tests
// can exercise both shapes against the same state.
func stateMoveFixture(t *testing.T, info Info) *local.Store {
	t.Helper()
	store, err := local.NewStore(
		".unobin/state", info.FactoryName, "default", encrypters.Noop{})
	require.NoError(t, err)
	stackInfo := state.FactoryInfo{
		Name: info.FactoryName, Version: info.FactoryVersion, ContentRevision: info.ContentRevision,
	}
	snap := state.NewSnapshot(stackInfo, "default")
	snap.Entries = []*state.Entry{
		{
			Address:     "resource.greeter.greeting.welcome",
			Type:        state.EntryLibraryCall,
			Library:     "greeter",
			LibraryType: "greeting",
		},
		{
			Address: "resource.greeter.greeting.welcome/resource.local.file.this",
			Type:    state.EntryLeaf,
			Kind:    "resource",
		},
		{
			Address: "resource.local.file.other",
			Type:    state.EntryLeaf,
			Kind:    "resource",
		},
	}
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
	return store
}

func snapshotAddresses(t *testing.T, store *local.Store) []string {
	t.Helper()
	snap, err := store.Current()
	require.NoError(t, err)
	out := make([]string, 0, len(snap.Entries))
	for _, e := range snap.Entries {
		out = append(out, e.Address)
	}
	return out
}

func TestStateMoveRelocatesLibraryCallSite(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	store := stateMoveFixture(t, info)

	out, err := runCfg(t, info, "state", "move",
		"resource.greeter.greeting.welcome", "resource.greeter.greeting.hello")
	require.NoError(t, err)
	require.Contains(t, out,
		"Moved resource.greeter.greeting.welcome"+
			" to resource.greeter.greeting.hello (2 entries).")

	require.ElementsMatch(t, []string{
		"resource.greeter.greeting.hello",
		"resource.greeter.greeting.hello/resource.local.file.this",
		"resource.local.file.other",
	}, snapshotAddresses(t, store))
}

func TestStateMoveSingleEntryLeavesLibraryAlone(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	store := stateMoveFixture(t, info)

	out, err := runCfg(t, info, "state", "move",
		"resource.local.file.other", "resource.local.file.renamed")
	require.NoError(t, err)
	require.Contains(t, out,
		"Moved resource.local.file.other to resource.local.file.renamed.")
	require.NotContains(t, out, "entries")

	require.ElementsMatch(t, []string{
		"resource.greeter.greeting.welcome",
		"resource.greeter.greeting.welcome/resource.local.file.this",
		"resource.local.file.renamed",
	}, snapshotAddresses(t, store))
}

func TestStateMoveBulkRejectsCollisionUnderTarget(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	store, err := local.NewStore(
		".unobin/state", info.FactoryName, "default", encrypters.Noop{})
	require.NoError(t, err)
	stackInfo := state.FactoryInfo{
		Name: info.FactoryName, Version: info.FactoryVersion, ContentRevision: info.ContentRevision,
	}
	snap := state.NewSnapshot(stackInfo, "default")
	snap.Entries = []*state.Entry{
		{
			Address:     "resource.greeter.greeting.a",
			Type:        state.EntryLibraryCall,
			Library:     "greeter",
			LibraryType: "greeting",
		},
		{
			Address: "resource.greeter.greeting.a/resource.local.file.this",
			Type:    state.EntryLeaf,
			Kind:    "resource",
		},
		{
			Address: "resource.greeter.greeting.b/resource.local.file.this",
			Type:    state.EntryLeaf,
			Kind:    "resource",
		},
	}
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	_, err = runCfg(t, info, "state", "move",
		"resource.greeter.greeting.a", "resource.greeter.greeting.b")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists at resource.greeter.greeting.b/resource.local.file.this")

	require.ElementsMatch(t, []string{
		"resource.greeter.greeting.a",
		"resource.greeter.greeting.a/resource.local.file.this",
		"resource.greeter.greeting.b/resource.local.file.this",
	}, snapshotAddresses(t, store))
}

func TestStateGCKeepsLatestPlusCurrent(t *testing.T) {
	info := testInfo(t, `actions: { core.echo.hi: { echo: 'hello' } }`)
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

	out, err := runCfg(t, info, "state", "gc", "--keep", "2")
	require.NoError(t, err)
	require.Contains(t, out, "Deleted 2 snapshot(s), kept 3.")

	after, err := store.List()
	require.NoError(t, err)
	require.Equal(t, []string{currentRev, revs[3], revs[4]}, after)
}

func TestStateGCNoOpWhenWithinKeep(t *testing.T) {
	info := testInfo(t, `actions: { core.echo.hi: { echo: 'hello' } }`)
	_ = applyVia(t, info, "")
	out, err := runCfg(t, info, "state", "gc", "--keep", "10")
	require.NoError(t, err)
	require.Contains(t, out, "Deleted 0 snapshot(s), kept 1.")
}

func TestStateForceUnlockReleasesLock(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hello' } }`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	store, err := local.NewStore(".unobin/state", info.FactoryName, "default",
		encrypters.Noop{})
	require.NoError(t, err)
	_, err = store.Lock(context.Background())
	require.NoError(t, err)

	out, err := runCfg(t, info, "state", "force-unlock")
	require.NoError(t, err)
	require.Contains(t, out, "Lock cleared.")

	again, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, again.Unlock())
}

func TestRefreshNoStateIsOK(t *testing.T) {
	info := testInfo(t, `actions: { core.echo.hi: { echo: 'hello' } }`)
	out, err := runCfg(t, info, "refresh", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "Refreshed 0, dropped 0.")
}

func TestRefreshCarriesActionsForward(t *testing.T) {
	info := testInfo(t, `
actions: { core.echo.hi: { echo: 'hello' } }
outputs: { said: { value: action.core.echo.hi.echo } }
`)
	_ = applyVia(t, info, "")

	out, err := runCfg(t, info, "refresh", "--allow-version-mismatch")
	require.NoError(t, err)
	require.Contains(t, out, "Refreshed 0, dropped 0.")

	show, err := runCfg(t, info, "state", "show")
	require.NoError(t, err)
	require.Contains(t, show, "action.core.echo.hi")
}

func TestStateListAndShow(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
outputs: { said: { value: action.core.echo.hi.echo } }
`
	info := testInfo(t, src)
	_ = applyVia(t, info, "")

	listOut, err := runCfg(t, info, "state", "list")
	require.NoError(t, err)
	require.Contains(t, listOut, "* ")

	showOut, err := runCfg(t, info, "state", "show")
	require.NoError(t, err)
	require.Contains(t, showOut, "factory:")
	require.Contains(t, showOut, "test-stack")
	require.Contains(t, showOut, "action.core.echo.hi")
	require.Contains(t, showOut, `said: 'hello'`)
}

func TestSchema(t *testing.T) {
	src := `
inputs: {
  greeting: {
    type:        string
    description: 'a friendly word'
  }
  size: {
    type:    integer
    default: 3
    minimum: 1
  }
  hosts: {
    type: list(string)
  }
}
`
	info := testInfo(t, src)
	out, err := runRoot(t, info, "schema")
	require.NoError(t, err)

	require.Contains(t, out, "greeting: string")
	require.Contains(t, out, "a friendly word")
	require.Contains(t, out, "size: integer")
	require.Contains(t, out, "default: 3")
	require.Contains(t, out, "hosts: list(string)")
}

func TestSchemaEmpty(t *testing.T) {
	info := testInfo(t, `description: 'x'`)
	out, err := runRoot(t, info, "schema")
	require.NoError(t, err)
	require.Contains(t, out, "No inputs declared.")
}

func TestStateEncryptedWithEnvKey(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello' } }
outputs: { said: { value: action.core.echo.hi.echo } }
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

	showOut, err := runBackend(t, info, "state", "show")
	require.NoError(t, err)
	require.Contains(t, showOut, `said: 'hello'`)
}

func TestStateShowFailsWithWrongKey(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hello' } }`
	info := testInfo(t, src)

	t.Setenv("UB_STATE_KEY", freshKeyB64(t))
	_ = applyVia(t, info, writeBackendConfig(t))

	t.Setenv("UB_STATE_KEY", freshKeyB64(t))
	_, err := runBackend(t, info, "state", "show")
	require.Error(t, err)
}

func TestLoadEncrypterRejectsBadKey(t *testing.T) {
	t.Setenv("UB_STATE_KEY", "not-base64!!")
	_, err := loadEncrypter(nil, "")
	require.Error(t, err)
}

func TestPlanFileEncryptedWithEnvKey(t *testing.T) {
	src := `
actions: { core.echo.hi: { echo: 'hello world' } }
outputs: { said: { value: action.core.echo.hi.echo } }
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
	require.Contains(t, string(plaintext), "action.core.echo.hi")

	out, err := runRoot(t, info, "apply", planFile)
	require.NoError(t, err)
	require.Contains(t, out, "said: 'hello world'")
}

func TestPlanFlagOverridesConfigParallelism(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hi' } }`
	info := testInfo(t, src)
	cfg := writeStateConfig(t, `
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
	src := `actions: { core.echo.hi: { echo: 'hi' } }`
	info := testInfo(t, src)
	cfg := writeStateConfig(t, `
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
	src := `actions: { core.echo.hi: { echo: 'hi' } }`
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

func TestApplyPlanWithoutKeyNamesMissingEnvVar(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hi' } }`
	info := testInfo(t, src)
	t.Setenv("UB_STATE_KEY", freshKeyB64(t))

	planFile := filepath.Join(t.TempDir(), "plan.enc")
	_, err := runBackend(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
	require.NoError(t, err)

	t.Setenv("UB_STATE_KEY", "")
	_, err = runRoot(t, info, "apply", planFile)
	require.Error(t, err)
	require.Contains(t, err.Error(), "UB_STATE_KEY is not set")
}

func TestPlanFilePlaintextWithoutEnvKey(t *testing.T) {
	src := `actions: { core.echo.hi: { echo: 'hi' } }`
	info := testInfo(t, src)

	planFile := filepath.Join(t.TempDir(), "plan.json")
	_, err := runCfg(t, info, "plan", "--allow-version-mismatch", "-o", planFile)
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

func TestPlanChecksForEachPredicate(t *testing.T) {
	src := `
inputs: {
  replicas: {
    type: list(object({ tls: optional(boolean) }))
    default: [{ tls: true }, { tls: false }]
  }
}
constraints: [
  {
    kind:      predicate
    @for-each: var.replicas
    when:      true
    require:   @each.value.tls == true
    message:   'tls is required'
  },
]
`
	info := testInfo(t, src)
	_, err := runRoot(t, info, "plan", "--allow-version-mismatch")
	require.Error(t, err)
	require.Contains(t, err.Error(), "tls is required (var.replicas[1])")
}

func TestLoadConfigInputsResolvesLocals(t *testing.T) {
	path := writeConfig(t, `
locals: {
  region: 'us-east-1'
  tags:   { team: 'core' }
}

factory: {
  inputs: {
    region: local.region
    name:   $'app-{{local.region}}'
    tags:   local.tags
  }
}
`)
	got, err := loadConfigInputs(parseTestConfig(t, path), path)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"region": "us-east-1",
		"name":   "app-us-east-1",
		"tags":   map[string]any{"team": "core"},
	}, got)
}

func TestApplyUIServesRunView(t *testing.T) {
	t.Setenv("BROWSER", "true")
	old := uiLingerTimeout
	uiLingerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { uiLingerTimeout = old })

	info := testInfo(t, `actions: { core.echo.hi: { echo: 'hello world' } }`)
	configPath := writeStateConfig(t, "")
	planFile := filepath.Join(t.TempDir(), "plan.json")
	_, err := runRoot(t, info,
		"plan", "--allow-version-mismatch", "-o", planFile, "-c", configPath)
	require.NoError(t, err)

	out, err := runRoot(t, info, "apply", "--ui", planFile)
	require.NoError(t, err)
	require.Regexp(t, `Run view: http://127\.0\.0\.1:\d+/[0-9a-f]{32}/`, out)
}
