package codegen

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

// encodeValue parses `v: <src>` and encodes the field value.
func encodeValue(t *testing.T, src string) string {
	t.Helper()
	f, err := lang.ParseSource("test.ub", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	got, err := EncodeNode(f.Body.Fields[0].Value)
	require.NoError(t, err)
	return got
}

func TestEncodeInterpolatedLiteralOnly(t *testing.T) {
	got := encodeValue(t, `$'hello'`)
	require.Equal(t,
		`&lang.InterpolatedString{Parts: []lang.InterpolatedPart{{Lit: "hello"}, }}`, got)
	parsesAsGoExpr(t, got)
}

func TestEncodeInterpolatedSlotWithVerb(t *testing.T) {
	got := encodeValue(t, `$'n-{{input.size:%03d}}'`)
	require.Contains(t, got, `{Lit: "n-"}`)
	require.Contains(t, got, `Expr: &lang.DotPath{`)
	require.Contains(t, got, `Verb: "%03d"`)
	parsesAsGoExpr(t, got)
}

func TestEncodeInterpolatedMixedParsesAsGo(t *testing.T) {
	got := encodeValue(t, `$'{{input.region}}/{{resource.aws.vpc.main.id}}-end'`)
	require.Contains(t, got, `{Lit: "-end"}`)
	parsesAsGoExpr(t, got)
}

func TestEncodeInterpolatedEmptyHasNoParts(t *testing.T) {
	got := encodeValue(t, `$''`)
	require.Equal(t, `&lang.InterpolatedString{Parts: []lang.InterpolatedPart{}}`, got)
	parsesAsGoExpr(t, got)
}
