package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefsLiteralHasNone(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "'just a string'")))
	require.Empty(t, Refs(parseValue(t, "42")))
	require.Empty(t, Refs(parseValue(t, "[1, 2, 3]")))
}

func TestRefsVarSimple(t *testing.T) {
	require.Equal(t, []string{"var.region"},
		Refs(parseValue(t, "var.region")))
}

func TestRefsVarFieldAccessIgnored(t *testing.T) {
	require.Equal(t, []string{"var.network"},
		Refs(parseValue(t, "var.network.vpc-id")))
}

func TestRefsResourceInstance(t *testing.T) {
	require.Equal(t, []string{"resource.aws.vpc.main"},
		Refs(parseValue(t, "resource.aws.vpc.main.id")))
}

func TestRefsResourceForEachIndex(t *testing.T) {
	got := Refs(parseValue(t, "resource.aws.instance.nodes['alpha'].id"))
	require.Equal(t, []string{"resource.aws.instance.nodes"}, got)
}

func TestRefsDataAndAction(t *testing.T) {
	require.Equal(t, []string{"data.aws.ami.ubuntu"},
		Refs(parseValue(t, "data.aws.ami.ubuntu.id")))
	require.Equal(t, []string{"action.core.command.smoke"},
		Refs(parseValue(t, "action.core.command.smoke.exit-code")))
}

func TestRefsEachIgnored(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "@each.value")))
	require.Empty(t, Refs(parseValue(t, "@each.key")))
}

func TestRefsInterpolated(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"empty string", `$''`, nil},
		{"literal only", `$'just text'`, nil},
		{"escaped braces are literal", `$'\{{not a ref}}'`, nil},
		{"each binding ignored", `$'{{@each.value}}'`, nil},
		{"single var", `$'{{var.region}}'`, []string{"var.region"}},
		{"single resource", `$'{{resource.aws.vpc.main.id}}'`, []string{"resource.aws.vpc.main"}},
		{"single data", `$'{{data.aws.ami.ubuntu.id}}'`, []string{"data.aws.ami.ubuntu"}},
		{
			"single action",
			`$'{{action.core.command.smoke.exit-code}}'`,
			[]string{"action.core.command.smoke"},
		},
		{"literal around ref", `$'https://{{var.host}}/v1'`, []string{"var.host"}},
		{"verb does not change ref", `$'{{var.n:%03d}}'`, []string{"var.n"}},
		{"nested field collapses to node", `$'{{var.network.vpc-id}}'`, []string{"var.network"}},
		{
			"for-each instance index",
			`$'{{resource.aws.instance.nodes['alpha'].id}}'`,
			[]string{"resource.aws.instance.nodes"},
		},
		{"two vars in order", `$'{{var.z}}-{{var.a}}'`, []string{"var.z", "var.a"}},
		{"duplicate ref deduped", `$'{{var.x}}/{{var.x}}'`, []string{"var.x"}},
		{
			"mixed kinds",
			`$'{{var.region}}/{{resource.aws.vpc.main.id}}/{{data.aws.ami.ubuntu.id}}'`,
			[]string{"var.region", "resource.aws.vpc.main", "data.aws.ami.ubuntu"},
		},
		{
			"conditional slot unions both branches",
			`$'{{if var.cond then resource.aws.vpc.main.id else data.aws.ami.ubuntu.id}}'`,
			[]string{"var.cond", "resource.aws.vpc.main", "data.aws.ami.ubuntu"},
		},
		{"call argument ref", `$'{{format('%s', var.x)}}'`, []string{"var.x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Refs(parseValue(t, tt.src)))
		})
	}
}

func TestRefsCollectsAcrossArrayAndObject(t *testing.T) {
	src := `[
  var.a,
  resource.aws.vpc.main.id,
  { sg: resource.aws.security-group.web.id, az: var.zone },
]`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{
		"var.a",
		"resource.aws.vpc.main",
		"resource.aws.security-group.web",
		"var.zone",
	}, got)
}

func TestRefsCollectsInsideCalls(t *testing.T) {
	src := `format('%s-%s', var.region, resource.aws.vpc.main.id)`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"var.region", "resource.aws.vpc.main"}, got)
}

func TestRefsCollectsAcrossOperators(t *testing.T) {
	src := `var.a + var.b > 0 && resource.aws.vpc.main.id == 'x'`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"var.a", "var.b", "resource.aws.vpc.main"}, got)
}

func TestRefsIndexExpressionIsScanned(t *testing.T) {
	src := `resource.aws.instance.nodes[var.key].id`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"resource.aws.instance.nodes", "var.key"}, got)
}

func TestRefsDeduplicates(t *testing.T) {
	src := `[var.region, var.region, resource.aws.vpc.main.id, resource.aws.vpc.main.cidr-block]`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"var.region", "resource.aws.vpc.main"}, got)
}

func TestRefsResourceWithoutThreeSegmentsIgnored(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "resource.aws.vpc")))
	require.Empty(t, Refs(parseValue(t, "resource.aws")))
}

func TestRefsUnknownRootIgnored(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "weird.thing.somewhere")))
}

func TestRefsInsideConditional(t *testing.T) {
	src := `if var.prod then resource.aws.vpc.big.id else resource.aws.vpc.small.id`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{
		"var.prod", "resource.aws.vpc.big", "resource.aws.vpc.small",
	}, got)
}

// A comprehension's bound name is not a node address, so it never
// becomes a dependency edge; only the source and any var/resource refs
// in the body or filter count.
func TestRefsInsideComprehensionExcludesBoundName(t *testing.T) {
	src := `[ for s in var.subnets : s.cidr-block when s.public ]`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"var.subnets"}, got)
}

func TestRefsInsideMapComprehension(t *testing.T) {
	src := `{ for s in resource.aws.subnet.all : s.id => var.tags }`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"resource.aws.subnet.all", "var.tags"}, got)
}
