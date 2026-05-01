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
