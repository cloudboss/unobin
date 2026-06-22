package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func localExprsFor(t *testing.T, m map[string]string) map[string]lang.Expr {
	t.Helper()
	out := make(map[string]lang.Expr, len(m))
	for k, v := range m {
		out[k] = parseValue(t, v)
	}
	return out
}

func TestRefsLiteralHasNone(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "'just a string'")))
	require.Empty(t, Refs(parseValue(t, "42")))
	require.Empty(t, Refs(parseValue(t, "[1, 2, 3]")))
}

func TestRefsVarSimple(t *testing.T) {
	require.Equal(t, []string{"input.region"},
		Refs(parseValue(t, "input.region")))
}

func TestRefsVarFieldAccessIgnored(t *testing.T) {
	require.Equal(t, []string{"input.network"},
		Refs(parseValue(t, "input.network.vpc-id")))
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
		{"single input", `$'{{input.region}}'`, []string{"input.region"}},
		{"single resource", `$'{{resource.aws.vpc.main.id}}'`, []string{"resource.aws.vpc.main"}},
		{"single data", `$'{{data.aws.ami.ubuntu.id}}'`, []string{"data.aws.ami.ubuntu"}},
		{
			"single action",
			`$'{{action.core.command.smoke.exit-code}}'`,
			[]string{"action.core.command.smoke"},
		},
		{"literal around ref", `$'https://{{input.host}}/v1'`, []string{"input.host"}},
		{"verb does not change ref", `$'{{input.n:%03d}}'`, []string{"input.n"}},
		{"nested field collapses to node", `$'{{input.network.vpc-id}}'`, []string{"input.network"}},
		{
			"for-each instance index",
			`$'{{resource.aws.instance.nodes['alpha'].id}}'`,
			[]string{"resource.aws.instance.nodes"},
		},
		{"two inputs in order", `$'{{input.z}}-{{input.a}}'`, []string{"input.z", "input.a"}},
		{"duplicate ref deduped", `$'{{input.x}}/{{input.x}}'`, []string{"input.x"}},
		{
			"mixed kinds",
			`$'{{input.region}}/{{resource.aws.vpc.main.id}}/{{data.aws.ami.ubuntu.id}}'`,
			[]string{"input.region", "resource.aws.vpc.main", "data.aws.ami.ubuntu"},
		},
		{
			"conditional slot unions both branches",
			`$'{{if input.cond then resource.aws.vpc.main.id else data.aws.ami.ubuntu.id}}'`,
			[]string{"input.cond", "resource.aws.vpc.main", "data.aws.ami.ubuntu"},
		},
		{"call argument ref", `$'{{format('%s', input.x)}}'`, []string{"input.x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Refs(parseValue(t, tt.src)))
		})
	}
}

func TestRefsCollectsAcrossArrayAndObject(t *testing.T) {
	src := `[
  input.a,
  resource.aws.vpc.main.id,
  { sg: resource.aws.security-group.web.id, az: input.zone },
]`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{
		"input.a",
		"resource.aws.vpc.main",
		"resource.aws.security-group.web",
		"input.zone",
	}, got)
}

func TestRefsCollectsInsideCalls(t *testing.T) {
	src := `format('%s-%s', input.region, resource.aws.vpc.main.id)`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"input.region", "resource.aws.vpc.main"}, got)
}

func TestRefsCollectsAcrossOperators(t *testing.T) {
	src := `input.a + input.b > 0 && resource.aws.vpc.main.id == 'x'`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"input.a", "input.b", "resource.aws.vpc.main"}, got)
}

func TestRefsIndexExpressionIsScanned(t *testing.T) {
	src := `resource.aws.instance.nodes[input.key].id`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"resource.aws.instance.nodes", "input.key"}, got)
}

func TestRefsDeduplicates(t *testing.T) {
	src := `[input.region, input.region, resource.aws.vpc.main.id, resource.aws.vpc.main.cidr-block]`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"input.region", "resource.aws.vpc.main"}, got)
}

func TestRefsResourceWithoutThreeSegmentsIgnored(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "resource.aws.vpc")))
	require.Empty(t, Refs(parseValue(t, "resource.aws")))
}

func TestRefsUnknownRootIgnored(t *testing.T) {
	require.Empty(t, Refs(parseValue(t, "weird.thing.somewhere")))
}

func TestRefsInsideConditional(t *testing.T) {
	src := `if input.prod then resource.aws.vpc.big.id else resource.aws.vpc.small.id`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{
		"input.prod", "resource.aws.vpc.big", "resource.aws.vpc.small",
	}, got)
}

// A comprehension's bound name is not a node address, so it never
// becomes a dependency edge; only the source and any input/resource refs
// in the body or filter count.
func TestRefsInsideComprehensionExcludesBoundName(t *testing.T) {
	src := `[ for s in input.subnets : s.cidr-block when s.public ]`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"input.subnets"}, got)
}

func TestRefsInsideMapComprehension(t *testing.T) {
	src := `{ for s in resource.aws.subnet.all : s.id => input.tags }`
	got := Refs(parseValue(t, src))
	require.Equal(t, []string{"resource.aws.subnet.all", "input.tags"}, got)
}

// Reading one field of an object-valued local depends only on that
// field's own source, not on every source the object references.
func TestRefsWithLocalsNarrowsObjectFieldToOneSource(t *testing.T) {
	ls := localExprsFor(t, map[string]string{
		"net": `{ vpc: resource.aws.vpc.main.id, sub: resource.aws.subnet.a.id }`,
	})
	got := refsWithLocals(parseValue(t, "local.net.vpc"), ls)
	require.Equal(t, []string{"resource.aws.vpc.main"}, got)
}

// Reading the whole local still depends on every source it references.
func TestRefsWithLocalsWholeReadExpandsEverySource(t *testing.T) {
	ls := localExprsFor(t, map[string]string{
		"net": `{ vpc: resource.aws.vpc.main.id, sub: resource.aws.subnet.a.id }`,
	})
	got := refsWithLocals(parseValue(t, "local.net"), ls)
	require.Equal(t, []string{"resource.aws.vpc.main", "resource.aws.subnet.a"}, got)
}

// A local that is itself a call cannot be narrowed statically, so
// navigating into it still depends on every source the call reads.
func TestRefsWithLocalsCallLocalStaysConservative(t *testing.T) {
	ls := localExprsFor(t, map[string]string{
		"merged": `merge(resource.aws.vpc.main.tags, resource.aws.subnet.a.tags)`,
	})
	got := refsWithLocals(parseValue(t, "local.merged.Name"), ls)
	require.Equal(t, []string{"resource.aws.vpc.main", "resource.aws.subnet.a"}, got)
}

// Navigating into a dot-path local preserves the trailed field in the
// placeholder path rather than collapsing to the local's own path.
func TestDeferredRefsPreservesFieldThroughDotPathLocal(t *testing.T) {
	ls := localExprsFor(t, map[string]string{
		"lb": `resource.aws.lb.main.endpoint`,
	})
	got := deferredRefs(parseValue(t, "local.lb.host"), ls)
	require.Equal(t, []string{"resource.aws.lb.main.endpoint.host"}, got)
}

func TestDeferredRefsRendersSplat(t *testing.T) {
	got := deferredRefs(parseValue(t, "resource.aws.vpc.main.subnets[*].cidr"), nil)
	require.Equal(t, []string{"resource.aws.vpc.main.subnets[*].cidr"}, got)
}

// Navigating into an object-valued local shows only the navigated
// field's source path in the placeholder.
func TestDeferredRefsNarrowsObjectField(t *testing.T) {
	ls := localExprsFor(t, map[string]string{
		"net": `{ vpc: resource.aws.vpc.main.id, sub: resource.aws.subnet.a.id }`,
	})
	got := deferredRefs(parseValue(t, "local.net.sub"), ls)
	require.Equal(t, []string{"resource.aws.subnet.a.id"}, got)
}

// A chain of dot-path locals grafts the trailing field all the way down.
func TestRefsWithLocalsChainsDotPathLocals(t *testing.T) {
	ls := localExprsFor(t, map[string]string{
		"outer": `local.inner.attrs`,
		"inner": `resource.aws.lb.main.config`,
	})
	require.Equal(t, []string{"resource.aws.lb.main"},
		refsWithLocals(parseValue(t, "local.outer.host"), ls))
	require.Equal(t, []string{"resource.aws.lb.main.config.attrs.host"},
		deferredRefs(parseValue(t, "local.outer.host"), ls))
}
