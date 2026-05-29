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
		name     string
		category string
		src      string
		want     []string
	}{
		{
			name:     "data with output and a data source",
			category: "data",
			src: `
data: { aws: { ami: { x: { most-recent: true } } } }
outputs: { id: { value: data.aws.ami.x.id } }
`,
		},
		{
			name:     "data pure compute with output only",
			category: "data",
			src:      `outputs: { v: { value: 'hi' } }`,
		},
		{
			name:     "data without output",
			category: "data",
			src:      `data: { aws: { ami: { x: { most-recent: true } } } }`,
			want:     []string{`composite "thing" (data): a data composite must declare at least one output`},
		},
		{
			name:     "data with a resource",
			category: "data",
			src: `
resources: { aws: { vpc: { m: {} } } }
outputs: { id: { value: 'x' } }
`,
			want: []string{`composite "thing" (data): a data composite must not contain resources`},
		},
		{
			name:     "data with an action",
			category: "data",
			src: `
actions: { core: { command: { c: { argv: ['x'] } } } }
outputs: { id: { value: 'x' } }
`,
			want: []string{`composite "thing" (data): a data composite must not contain actions`},
		},
		{
			name:     "data with every violation",
			category: "data",
			src: `
resources: { aws: { vpc: { m: {} } } }
actions: { core: { command: { c: { argv: ['x'] } } } }
`,
			want: []string{
				`composite "thing" (data): a data composite must declare at least one output`,
				`composite "thing" (data): a data composite must not contain resources`,
				`composite "thing" (data): a data composite must not contain actions`,
			},
		},
		{
			name:     "data empty body",
			category: "data",
			src:      `description: 'x'`,
			want:     []string{`composite "thing" (data): a data composite must declare at least one output`},
		},
		{
			name:     "action with an action",
			category: "action",
			src:      `actions: { core: { command: { c: { argv: ['x'] } } } }`,
		},
		{
			name:     "action with data is allowed",
			category: "action",
			src: `
data: { aws: { ami: { x: {} } } }
actions: { core: { command: { c: { argv: ['x'] } } } }
`,
		},
		{
			name:     "action without an action",
			category: "action",
			src:      `outputs: { v: { value: 'x' } }`,
			want:     []string{`composite "thing" (action): an action composite must contain at least one action`},
		},
		{
			name:     "action with a resource",
			category: "action",
			src: `
resources: { aws: { vpc: { m: {} } } }
actions: { core: { command: { c: { argv: ['x'] } } } }
`,
			want: []string{`composite "thing" (action): an action composite must not contain resources`},
		},
		{
			name:     "action with a resource and no action",
			category: "action",
			src:      `resources: { aws: { vpc: { m: {} } } }`,
			want: []string{
				`composite "thing" (action): an action composite must contain at least one action`,
				`composite "thing" (action): an action composite must not contain resources`,
			},
		},
		{
			name:     "resource with a resource",
			category: "resource",
			src:      `resources: { aws: { vpc: { m: {} } } }`,
		},
		{
			name:     "resource with data and actions is allowed",
			category: "resource",
			src: `
resources: { aws: { vpc: { m: {} } } }
data: { aws: { ami: { x: {} } } }
actions: { core: { command: { c: { argv: ['x'] } } } }
`,
		},
		{
			name:     "resource without a resource",
			category: "resource",
			src:      `data: { aws: { ami: { x: {} } } }`,
			want:     []string{`composite "thing" (resource): a resource composite must contain at least one resource`},
		},
		{
			name:     "resource empty body",
			category: "resource",
			src:      `description: 'x'`,
			want:     []string{`composite "thing" (resource): a resource composite must contain at least one resource`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateCompositeBody(tt.category, "thing", parseComposite(t, tt.src))
			var msgs []string
			for _, e := range got {
				msgs = append(msgs, e.Error())
			}
			require.Equal(t, tt.want, msgs)
		})
	}
}
