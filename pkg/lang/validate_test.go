package lang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseWithKind(t *testing.T, src string, kind FileKind) *File {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	f.Kind = kind
	return f
}

func TestValidateTopLevelKeysStack(t *testing.T) {
	src := `
description: 'test'
inputs:      {}
constraints: []
imports:     {}
data:        {}
resources:   {}
actions:     {}
outputs:     {}
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 0, errs.Len(), "expected no errors, got: %v", errs.Errors())
}

func TestValidateTopLevelKeysModule(t *testing.T) {
	src := `
description: 'a module'
exports:     { cluster: 'cluster.ub' }
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileModule))
	require.Equal(t, 0, errs.Len())
}

func TestValidateTopLevelKeysConfig(t *testing.T) {
	src := `
stack:          { source: 'github.com/x/y' }
deployment-id:  'prod'
parallelism:    10
state:          { backend: local }
inputs:         { region: 'us-east-1' }
configurations: { aws: { default: {} } }
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileConfig))
	require.Equal(t, 0, errs.Len())
}

func TestValidateRejectsForeignKeys(t *testing.T) {
	cases := []struct {
		name   string
		kind   FileKind
		src    string
		badKey string
	}{
		{
			name:   "stack-with-exports",
			kind:   FileStack,
			src:    "exports: { x: 'y.ub' }\n",
			badKey: "exports",
		},
		{
			name:   "module-with-inputs",
			kind:   FileModule,
			src:    "inputs: {}\n",
			badKey: "inputs",
		},
		{
			name:   "config-with-resources",
			kind:   FileConfig,
			src:    "resources: {}\n",
			badKey: "resources",
		},
		{
			name:   "stack-with-state",
			kind:   FileStack,
			src:    "state: { backend: local }\n",
			badKey: "state",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := ValidateTopLevelKeys(parseWithKind(t, c.src, c.kind))
			require.Equal(t, 1, errs.Len())
			require.Contains(t, errs.Errors()[0].Msg, c.badKey)
		})
	}
}

func TestValidateRejectsMetaKey(t *testing.T) {
	src := "@module: 'aws'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "@-prefixed")
}

func TestValidateRejectsStringKey(t *testing.T) {
	src := "'description': 'x'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "top level key must be an identifier")
}

func TestValidateRejectsDuplicateKey(t *testing.T) {
	src := `
description: 'first'
description: 'second'
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate")
}

func TestValidateUnknownKindRefuses(t *testing.T) {
	src := "description: 'x'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileUnknown))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "unknown")
}

func TestValidateCollectsMultiple(t *testing.T) {
	src := `
exports:    { x: 'y.ub' }
state:      { backend: local }
@bad:       1
'quoted':   2
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 4, errs.Len(), "expected 4 errors, got: %s",
		strings.Join(errsToStrings(errs), "; "))
}

func errsToStrings(l *ErrorList) []string {
	es := l.Errors()
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Error()
	}
	return out
}
