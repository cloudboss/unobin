package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type endpointConfiguration struct {
	Endpoint *cfg.String
}

type requiredEndpointConfiguration struct {
	Endpoint cfg.String
}

type runtimePlainConfiguration struct {
	Endpoint string
}

type runtimeReferenceConfiguration struct {
	Tags  map[string]string
	Items []string
}

type echoResource struct {
	Value string
}

func (r *echoResource) SchemaVersion() int { return 1 }
func (r *echoResource) Create(_ context.Context, _ any) (any, error) {
	return map[string]any{"value": r.Value, "id": "id-" + r.Value}, nil
}
func (r *echoResource) Read(_ context.Context, _ any, prior any) (any, error) { return prior, nil }
func (r *echoResource) Update(_ context.Context, _ any, _ Prior[echoResource, any]) (any, error) {
	return map[string]any{"value": r.Value, "id": "id-" + r.Value}, nil
}
func (r *echoResource) Delete(_ context.Context, _ any, _ any) error { return nil }
func (r *echoResource) ReplaceFields() []string                      { return []string{"value"} }

type configEchoResource struct {
	readSeen   *[]string
	deleteSeen *[]string
}

func endpointOf(c any) string {
	switch conf := c.(type) {
	case *endpointConfiguration:
		if conf == nil || conf.Endpoint == nil {
			return ""
		}
		return conf.Endpoint.Value
	case *requiredEndpointConfiguration:
		if conf == nil {
			return ""
		}
		return conf.Endpoint.Value
	case *runtimePlainConfiguration:
		if conf == nil {
			return ""
		}
		return conf.Endpoint
	default:
		return ""
	}
}

func (r *configEchoResource) SchemaVersion() int { return 1 }
func (r *configEchoResource) Create(_ context.Context, c any) (any, error) {
	return map[string]any{"endpoint": endpointOf(c)}, nil
}
func (r *configEchoResource) Read(_ context.Context, c any, prior any) (any, error) {
	if r.readSeen != nil {
		*r.readSeen = append(*r.readSeen, endpointOf(c))
	}
	return prior, nil
}
func (r *configEchoResource) Update(
	_ context.Context, c any, _ Prior[configEchoResource, any],
) (any, error) {
	return map[string]any{"endpoint": endpointOf(c)}, nil
}
func (r *configEchoResource) Delete(_ context.Context, c any, _ any) error {
	if r.deleteSeen != nil {
		*r.deleteSeen = append(*r.deleteSeen, endpointOf(c))
	}
	return nil
}
func (r *configEchoResource) ReplaceFields() []string { return nil }

func configuredLibraries() map[string]*Library {
	return configuredLibrariesRecording(nil, nil)
}

func configuredLibrariesRecording(readSeen, deleteSeen *[]string) map[string]*Library {
	return configuredLibrariesWithConfig(
		func() any { return &endpointConfiguration{} }, readSeen, deleteSeen)
}

func requiredConfiguredLibraries() map[string]*Library {
	return configuredLibrariesWithConfig(
		func() any { return &requiredEndpointConfiguration{} }, nil, nil)
}

func plainConfiguredLibraries(schema *LibrarySchema) map[string]*Library {
	libs := configuredLibrariesWithConfig(
		func() any { return &runtimePlainConfiguration{} }, nil, nil)
	libs["fix"].Schema = schema
	return libs
}

func referenceConfigLibrary(schema *LibrarySchema) *Library {
	return &Library{
		Configuration: &cfg.ConfigurationType[*runtimeReferenceConfiguration]{
			New: func() *runtimeReferenceConfiguration {
				return &runtimeReferenceConfiguration{}
			},
		},
		Schema: schema,
	}
}

func referenceConfigSchema(defaultTags string, constrained bool) *LibrarySchema {
	fields := []typecheck.ObjectField{
		{Name: "tags", Type: typecheck.TMap(typecheck.TString()), Defaulted: true},
		{Name: "items", Type: typecheck.TList(typecheck.TString()), Defaulted: true},
	}
	defaults := []lang.DefaultSpec{
		{Field: "input.tags", Value: defaultTags},
		{Field: "input.items", Value: "['a', 'b']"},
	}
	var constraints []lang.ConstraintSpec
	if constrained {
		constraints = []lang.ConstraintSpec{{
			Kind:    "predicate",
			When:    "true",
			Require: "(@core.length(input.tags) >= 1)",
			Message: "tags are required",
		}}
	}
	return &LibrarySchema{
		HasConfiguration:         true,
		ConfigurationFields:      fields,
		ConfigurationDefaults:    defaults,
		ConfigurationConstraints: constraints,
		ConfigurationDigest:      cfg.DigestView(fields, defaults, constraints),
	}
}

func plainEndpointSchema(defaulted, constrained bool) *LibrarySchema {
	fields := []typecheck.ObjectField{{Name: "endpoint", Type: typecheck.TString()}}
	var defaults []lang.DefaultSpec
	if defaulted {
		fields[0].Defaulted = true
		defaults = []lang.DefaultSpec{{Field: "input.endpoint", Value: "'https://default.example'"}}
	}
	var constraints []lang.ConstraintSpec
	if constrained {
		constraints = []lang.ConstraintSpec{{
			Kind:    "predicate",
			When:    "true",
			Require: "(@core.length(input.endpoint) >= 1)",
			Message: "endpoint is required",
		}}
	}
	return &LibrarySchema{
		HasConfiguration:         true,
		ConfigurationFields:      fields,
		ConfigurationDefaults:    defaults,
		ConfigurationConstraints: constraints,
		ConfigurationDigest:      cfg.DigestView(fields, defaults, constraints),
	}
}

func configuredLibrariesWithConfig(
	newConfig func() any,
	readSeen, deleteSeen *[]string,
) map[string]*Library {
	return map[string]*Library{
		"base": {
			Name: "base",
			Resources: map[string]ResourceRegistration{
				"echo": MakeResource[echoResource, any, any](),
			},
		},
		"fix": {
			Name: "fix",
			Configuration: &cfg.ConfigurationType[any]{
				New: newConfig,
			},
			Resources: map[string]ResourceRegistration{
				"echo": MakeResource[echoResource, any, any](),
				"config-echo": MakeResourceWith[configEchoResource, any, any](
					func() *configEchoResource {
						return &configEchoResource{readSeen: readSeen, deleteSeen: deleteSeen}
					},
				),
			},
		},
	}
}

func configurationTestExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
) *Executor {
	t.Helper()
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{DAG: dag, SyntaxSource: syntaxSource, Libraries: libs}
}

func TestApplyEvaluatesLibraryConfigBinding(t *testing.T) {
	libs := configuredLibraries()
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "direct-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Inputs = map[string]any{
		"fix-config": map[string]any{"endpoint": "https://alias.example"},
	}
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	res := applyOnce(t, exec)
	require.Equal(t, "https://alias.example", res.Outputs["got"])
}

func TestRefreshUsesLibraryConfigBinding(t *testing.T) {
	var reads []string
	libs := configuredLibrariesRecording(&reads, nil)
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "direct-config")
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	first := configurationTestExecutor(t, src, libs)
	first.Inputs = map[string]any{
		"fix-config": map[string]any{"endpoint": "https://first.example"},
	}
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	reads = nil
	fresh := configurationTestExecutor(t, src, libs)
	fresh.Inputs = map[string]any{
		"fix-config": map[string]any{"endpoint": "https://fresh.example"},
	}
	fresh.Store = store
	fresh.Factory = factory
	_, err := fresh.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://fresh.example"}, reads)
}

func TestApplyEvaluatesDerivedLibraryConfig(t *testing.T) {
	libs := configuredLibraries()
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "derived-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	res := applyOnce(t, exec)
	require.Equal(t, "https://cluster.example", res.Outputs["got"])
}

func TestPlanAppliesLibraryConfigDefaultsBeforeDecode(t *testing.T) {
	libs := plainConfiguredLibraries(plainEndpointSchema(true, false))
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "default-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	res := applyOnce(t, exec)
	require.Equal(t, "https://default.example", res.Outputs["got"])
}

func TestDecodeLibraryConfigAppliesReferenceDefaults(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", false))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{})

	require.NoError(t, err)
	got := gotAny.(*runtimeReferenceConfiguration)
	require.Equal(t, map[string]string{"env": "dev"}, got.Tags)
	require.Equal(t, []string{"a", "b"}, got.Items)
}

func TestDecodeLibraryConfigKeepsReferenceValues(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", false))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{
		"tags":  map[string]any{"env": "prod"},
		"items": []any{"x"},
	})

	require.NoError(t, err)
	got := gotAny.(*runtimeReferenceConfiguration)
	require.Equal(t, map[string]string{"env": "prod"}, got.Tags)
	require.Equal(t, []string{"x"}, got.Items)
}

func TestDecodeLibraryConfigRejectsNullReferenceValue(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", false))

	_, err := decodeLibraryConfig(lib, map[string]any{"tags": nil})

	require.Error(t, err)
	require.Contains(t, err.Error(), `field "tags": required but is null`)
}

func TestDecodeLibraryConfigConstraintsSeeReferenceDefaults(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", true))

	_, err := decodeLibraryConfig(lib, map[string]any{})

	require.NoError(t, err)
}

func TestPlanChecksLibraryConfigConstraints(t *testing.T) {
	libs := plainConfiguredLibraries(plainEndpointSchema(false, true))
	src := ubtest.ReadInvalidFixture(t, "testdata/ub/apply-configuration", "constraint-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "library-config.fix")
	require.Contains(t, err.Error(), "endpoint is required")
}

func TestPlanRejectsMissingLibraryConfigField(t *testing.T) {
	libs := plainConfiguredLibraries(plainEndpointSchema(false, false))
	src := ubtest.ReadInvalidFixture(t, "testdata/ub/apply-configuration", "empty-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "library-config.fix")
	require.Contains(t, err.Error(), "endpoint")
	require.Contains(t, err.Error(), "required")
}

func TestPlanRejectsUnknownLibraryConfigField(t *testing.T) {
	libs := plainConfiguredLibraries(plainEndpointSchema(false, false))
	src := ubtest.ReadInvalidFixture(t, "testdata/ub/apply-configuration", "unknown-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "library-config.fix")
	require.Contains(t, err.Error(), "typo")
}

func TestPlanRejectsWrongLibraryConfigFieldType(t *testing.T) {
	libs := plainConfiguredLibraries(plainEndpointSchema(false, false))
	src := ubtest.ReadInvalidFixture(t, "testdata/ub/apply-configuration", "wrong-type-config")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "library-config.fix")
	require.Contains(t, err.Error(), "endpoint")
	require.Contains(t, err.Error(), "string")
}

func TestRequiredLibraryConfigReportsDecodeError(t *testing.T) {
	src := ubtest.ReadInvalidFixture(t, "testdata/ub/apply-configuration", "required-config-missing")
	libs := requiredConfiguredLibraries()
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	_, err := exec.Plan(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "library-config.fix")
	require.Contains(t, err.Error(), "endpoint")
}

func TestPlanEvaluatesLibraryConfigFromState(t *testing.T) {
	var seen []string
	libs := configuredLibrariesRecording(&seen, nil)
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "derived-config")
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	first := configurationTestExecutor(t, src, libs)
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	seen = nil
	fresh := configurationTestExecutor(t, src, libs)
	fresh.Store = store
	fresh.Factory = factory
	plan, err := fresh.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, seen)
	require.Equal(t, DecisionNoOp, findStep(t, plan, "resource.app").Decision)
}

type configProbeData struct {
	readSeen *[]string
}

func (d *configProbeData) Read(_ context.Context, c any) (any, error) {
	if d.readSeen != nil {
		*d.readSeen = append(*d.readSeen, endpointOf(c))
	}
	return map[string]any{"value": "v"}, nil
}

func TestDataReadDefersWhileLibraryConfigPending(t *testing.T) {
	var seen []string
	libs := configuredLibrariesRecording(&seen, nil)
	libs["fix"].DataSources = map[string]DataSourceRegistration{
		"probe": MakeDataSourceWith[configProbeData, any, any](
			func() *configProbeData { return &configProbeData{readSeen: &seen} },
		),
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "data-config-pending")
	exec := configurationTestExecutor(t, src, libs)
	exec.Store = newStateStore(t)
	exec.Factory = state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Empty(t, seen)
	ds := findStep(t, plan, "data-source.p")
	require.Equal(t, "library-config.fix", ds.DeferredConfig)

	fresh := configurationTestExecutor(t, src, libs)
	fresh.Store = exec.Store
	fresh.Factory = exec.Factory
	_, err = planAndApply(fresh)
	require.NoError(t, err)
	require.Equal(t, []string{"id-https://cluster.example"}, seen)
}

func TestDriftReadSkippedWhileLibraryConfigPending(t *testing.T) {
	var seen []string
	libs := configuredLibrariesRecording(&seen, nil)
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "input-config")
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	first := configurationTestExecutor(t, src, libs)
	first.Inputs = map[string]any{"url": "https://a"}
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	seen = nil
	fresh := configurationTestExecutor(t, src, libs)
	fresh.Inputs = map[string]any{"url": "https://b"}
	fresh.Store = store
	fresh.Factory = factory
	plan, err := fresh.Plan(context.Background())
	require.NoError(t, err)
	require.Empty(t, seen)
	step := findStep(t, plan, "resource.app")
	require.Equal(t, "library-config.fix", step.DeferredConfig)
	require.Equal(t, DecisionNoOp, step.Decision)
}

func TestDestroyUsesLibraryConfigFromState(t *testing.T) {
	var deletes []string
	libs := configuredLibrariesRecording(nil, &deletes)
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "derived-config")
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	first := configurationTestExecutor(t, src, libs)
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	down := configurationTestExecutor(t, src, libs)
	down.Store = store
	down.Factory = factory
	down.Destroy = true
	_, err := planAndApply(down)
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, deletes)
}

func TestRefreshUsesLibraryConfigFromState(t *testing.T) {
	var reads []string
	libs := configuredLibrariesRecording(&reads, nil)
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-configuration", "derived-config")
	store := newStateStore(t)
	factory := state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"}
	first := configurationTestExecutor(t, src, libs)
	first.Store = store
	first.Factory = factory
	applyOnce(t, first)

	reads = nil
	fresh := configurationTestExecutor(t, src, libs)
	fresh.Store = store
	fresh.Factory = factory
	_, err := fresh.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"https://cluster.example"}, reads)
}

func TestSyntaxValidationRejectsOldConfigurationMeta(t *testing.T) {
	src := ubtest.ReadFixture(t,
		"testdata/ub/apply-configuration/invalid/old-configuration-meta.ub")
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	errs := syntax.ValidateFile(f)
	require.Error(t, errs.Err())
	require.Contains(t, errs.Err().Error(), `meta key "@configuration" is not allowed`)
}
