package resolve

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func parseSyntaxComposite(t *testing.T, kind, src string) syntax.CompositeDecl {
	t.Helper()
	f, err := syntax.ParseSource("library.ub", fmt.Appendf(nil, "thing: %s {\n%s\n}\n", kind, src))
	require.NoError(t, err)
	require.NotNil(t, f.Library)
	require.Len(t, f.Library.Exports, 1)
	return f.Library.Exports[0]
}

func TestValidateSyntaxCompositeBody(t *testing.T) {
	tests := []struct {
		name string
		kind string
		src  string
		want []string
	}{
		{
			name: "data with output and a data source",
			kind: "data",
			src: `
data: { x: aws.ami { most-recent: true } }
outputs: { id: { value: data.x.id } }
`,
		},
		{
			name: "data pure compute with output only",
			kind: "data",
			src:  `outputs: { v: { value: 'hi' } }`,
		},
		{
			name: "data without output",
			kind: "data",
			src:  `data: { x: aws.ami { most-recent: true } }`,
			want: []string{`composite "thing" (data): a data composite must declare at least one output`},
		},
		{
			name: "data with a resource",
			kind: "data",
			src: `
resources: { m: aws.vpc {} }
outputs:   { id: { value: 'x' } }
`,
			want: []string{`composite "thing" (data): a data composite must not contain resources`},
		},
		{
			name: "data with an action",
			kind: "data",
			src: `
actions: { c: core.command { argv: ['x'] } }
outputs: { id: { value: 'x' } }
`,
			want: []string{`composite "thing" (data): a data composite must not contain actions`},
		},
		{
			name: "data with every violation",
			kind: "data",
			src: `
resources: { m: aws.vpc {} }
actions:   { c: core.command { argv: ['x'] } }
`,
			want: []string{
				`composite "thing" (data): a data composite must declare at least one output`,
				`composite "thing" (data): a data composite must not contain resources`,
				`composite "thing" (data): a data composite must not contain actions`,
			},
		},
		{
			name: "data empty body",
			kind: "data",
			src:  `description: 'x'`,
			want: []string{`composite "thing" (data): a data composite must declare at least one output`},
		},
		{
			name: "action with an action",
			kind: "action",
			src:  `actions: { c: core.command { argv: ['x'] } }`,
		},
		{
			name: "action with data is accepted",
			kind: "action",
			src: `
data:    { x: aws.ami {} }
actions: { c: core.command { argv: ['x'] } }
`,
		},
		{
			name: "action without an action",
			kind: "action",
			src:  `outputs: { v: { value: 'x' } }`,
			want: []string{
				`composite "thing" (action): an action composite must contain at least one action`,
			},
		},
		{
			name: "action with a resource",
			kind: "action",
			src: `
resources: { m: aws.vpc {} }
actions:   { c: core.command { argv: ['x'] } }
`,
			want: []string{`composite "thing" (action): an action composite must not contain resources`},
		},
		{
			name: "action with a resource and no action",
			kind: "action",
			src:  `resources: { m: aws.vpc {} }`,
			want: []string{
				`composite "thing" (action): an action composite must contain at least one action`,
				`composite "thing" (action): an action composite must not contain resources`,
			},
		},
		{
			name: "resource with a resource",
			kind: "resource",
			src:  `resources: { m: aws.vpc {} }`,
		},
		{
			name: "resource with data and actions is accepted",
			kind: "resource",
			src: `
resources: { m: aws.vpc {} }
data:      { x: aws.ami {} }
actions:   { c: core.command { argv: ['x'] } }
`,
		},
		{
			name: "resource without a resource",
			kind: "resource",
			src:  `data: { x: aws.ami {} }`,
			want: []string{
				`composite "thing" (resource): a resource composite must contain at least one resource`,
			},
		},
		{
			name: "resource empty body",
			kind: "resource",
			src:  `description: 'x'`,
			want: []string{
				`composite "thing" (resource): a resource composite must contain at least one resource`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			export := parseSyntaxComposite(t, tt.kind, tt.src)
			got := ValidateSyntaxCompositeBody(string(export.Kind), export.Name.Name, export.Body)
			var msgs []string
			for _, e := range got {
				msgs = append(msgs, e.Error())
			}
			require.Equal(t, tt.want, msgs)
		})
	}
}
