package resolve

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func parseComposite(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("x.ub", []byte(src))
	require.NoError(t, err)
	return f
}

func TestValidateCompositeBody(t *testing.T) {
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
data:    { aws.ami.x: { most-recent: true } }
outputs: { id: { value: data.aws.ami.x.id } }
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
			src:  `data: { aws.ami.x: { most-recent: true } }`,
			want: []string{`composite "thing" (data): a data composite must declare at least one output`},
		},
		{
			name: "data with a resource",
			kind: "data",
			src: `
resources: { aws.vpc.m: {} }
outputs:   { id: { value: 'x' } }
`,
			want: []string{`composite "thing" (data): a data composite must not contain resources`},
		},
		{
			name: "data with an action",
			kind: "data",
			src: `
actions: { core.command.c: { argv: ['x'] } }
outputs: { id: { value: 'x' } }
`,
			want: []string{`composite "thing" (data): a data composite must not contain actions`},
		},
		{
			name: "data with every violation",
			kind: "data",
			src: `
resources: { aws.vpc.m: {} }
actions:   { core.command.c: { argv: ['x'] } }
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
			src:  `actions: { core.command.c: { argv: ['x'] } }`,
		},
		{
			name: "action with data is allowed",
			kind: "action",
			src: `
data:    { aws.ami.x: {} }
actions: { core.command.c: { argv: ['x'] } }
`,
		},
		{
			name: "action without an action",
			kind: "action",
			src:  `outputs: { v: { value: 'x' } }`,
			want: []string{`composite "thing" (action): an action composite must contain at least one action`},
		},
		{
			name: "action with a resource",
			kind: "action",
			src: `
resources: { aws.vpc.m: {} }
actions:   { core.command.c: { argv: ['x'] } }
`,
			want: []string{`composite "thing" (action): an action composite must not contain resources`},
		},
		{
			name: "action with a resource and no action",
			kind: "action",
			src:  `resources: { aws.vpc.m: {} }`,
			want: []string{
				`composite "thing" (action): an action composite must contain at least one action`,
				`composite "thing" (action): an action composite must not contain resources`,
			},
		},
		{
			name: "resource with a resource",
			kind: "resource",
			src:  `resources: { aws.vpc.m: {} }`,
		},
		{
			name: "resource with data and actions is allowed",
			kind: "resource",
			src: `
resources: { aws.vpc.m: {} }
data:      { aws.ami.x: {} }
actions:   { core.command.c: { argv: ['x'] } }
`,
		},
		{
			name: "resource without a resource",
			kind: "resource",
			src:  `data: { aws.ami.x: {} }`,
			want: []string{`composite "thing" (resource): a resource composite must contain at least one resource`},
		},
		{
			name: "resource empty body",
			kind: "resource",
			src:  `description: 'x'`,
			want: []string{`composite "thing" (resource): a resource composite must contain at least one resource`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateCompositeBody(tt.kind, "thing", parseComposite(t, tt.src))
			var msgs []string
			for _, e := range got {
				msgs = append(msgs, e.Error())
			}
			require.Equal(t, tt.want, msgs)
		})
	}
}
