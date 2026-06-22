package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func interpolatedScope() *Scope {
	return &Scope{
		Inputs: []ObjectField{
			{Name: "region", Type: TString()},
			{Name: "count", Type: TInteger()},
			{Name: "ratio", Type: TNumber()},
			{Name: "flag", Type: TBoolean()},
			{Name: "ports", Type: TList(TInteger())},
			{Name: "tags", Type: TMap(TString())},
			{Name: "cfg", Type: TObject([]ObjectField{{Name: "k", Type: TString()}})},
			{Name: "maybe", Type: TString(), Optional: true},
			{Name: "sized", Type: TInteger(), Optional: true, Defaulted: true},
			{Name: "anything", Type: TOpaque()},
		},
	}
}

func TestInferInterpolatedResultIsString(t *testing.T) {
	for _, src := range []string{
		`$'literal only'`,
		`$''`,
		`$'{{input.region}}'`,
		`$'a-{{input.count}}-{{input.region}}'`,
	} {
		errs := lang.NewErrorList(0)
		got := Infer(parseExpr(t, src), TUnknown(), interpolatedScope(), errs)
		assert.True(t, got.Equal(TString()), "%s -> %s", src, got)
		assert.Empty(t, errs.Errors(), "%s should not error", src)
	}
}

func TestInferInterpolatedScalarSlotsAccepted(t *testing.T) {
	// Scalars are fine; an unresolved node fails open so the runtime
	// guard handles it rather than the type checker.
	srcs := []string{
		`$'{{input.region}}'`,
		`$'{{input.count}}'`,
		`$'{{input.ratio}}'`,
		`$'{{input.flag}}'`,
		`$'{{input.count:%03d}}'`,
		`$'{{input.count + 1}}'`,
		`$'{{if input.flag then input.region else 'other'}}'`,
		`$'{{resource.aws.vpc.main.id}}'`,
		// A defaulted input reads as its inner type because omission
		// fills a non-null value before the expression reads it.
		`$'{{input.sized}}'`,
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, src), TUnknown(), interpolatedScope(), errs)
			assert.Empty(t, errs.Errors(), "%s should not error: %v", src, errs.Errors())
		})
	}
}

func TestInferInterpolatedRejectsBadSlot(t *testing.T) {
	tests := []struct {
		name string
		src  string
		msg  string
	}{
		{"list", `$'{{input.ports}}'`, "must be a scalar"},
		{"map", `$'{{input.tags}}'`, "must be a scalar"},
		{"object", `$'{{input.cfg}}'`, "must be a scalar"},
		{"optional", `$'{{input.maybe}}'`, "may be null"},
		{"null literal", `$'{{null}}'`, "may be null"},
		{
			"opaque", `$'{{input.anything}}'`,
			"interpolation slot is opaque; render it as text with @core.to-json(x)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, tt.src), TUnknown(), interpolatedScope(), errs)
			assert.True(t, got.Equal(TString()), "result is still string")
			require.Len(t, errs.Errors(), 1)
			assert.Contains(t, errs.Errors()[0].Msg, tt.msg)
		})
	}
}

func TestInferInterpolatedReportsEachBadSlot(t *testing.T) {
	// Two bad slots in one string produce two separate diagnostics.
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, `$'{{input.ports}}-{{input.tags}}'`), TUnknown(), interpolatedScope(), errs)
	require.Len(t, errs.Errors(), 2)
}
