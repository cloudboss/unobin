package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func splatScope() *Scope {
	return &Scope{
		Inputs: []ObjectField{
			{Name: "subnets", Type: TList(TObject([]ObjectField{
				{Name: "id", Type: TString()},
				{Name: "cidr", Type: TString()},
				{Name: "public", Type: TBoolean()},
			}))},
			{Name: "grid", Type: TList(TList(TObject([]ObjectField{
				{Name: "name", Type: TString()},
			})))},
			{Name: "servers", Type: TList(TObject([]ObjectField{
				{Name: "meta", Type: TObject([]ObjectField{{Name: "name", Type: TString()}})},
				{Name: "ports", Type: TList(TInteger())},
			}))},
			{Name: "regions", Type: TList(TObject([]ObjectField{
				{Name: "subnets", Type: TList(TObject([]ObjectField{{Name: "id", Type: TString()}}))},
			}))},
			{Name: "nums", Type: TList(TInteger())},
			{Name: "m", Type: TMap(TString())},
			{Name: "maybe", Type: TOptional(TList(TString()))},
			{Name: "whatever", Type: TList(TAny())},
		},
	}
}

func TestInferSplat(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want Type
	}{
		{name: "project string field", src: "var.subnets[*].id", want: TList(TString())},
		{name: "project bool field", src: "var.subnets[*].public", want: TList(TBoolean())},
		{name: "project nested object field", src: "var.servers[*].meta.name", want: TList(TString())},
		{name: "splat then list field", src: "var.servers[*].ports", want: TList(TList(TInteger()))},
		{name: "splat then field then index", src: "var.servers[*].ports[0]", want: TList(TInteger())},
		{name: "nested splat", src: "var.grid[*][*].name", want: TList(TList(TString()))},
		{
			name: "field then splat under splat",
			src:  "var.regions[*].subnets[*].id",
			want: TList(TList(TString())),
		},
		{name: "bare splat types as the list", src: "var.nums[*]", want: TList(TInteger())},
		{name: "splat over optional list", src: "var.maybe[*]", want: TList(TString())},
		{name: "splat over list of any", src: "var.whatever[*]", want: TList(TAny())},
		{name: "splat then field on any", src: "var.whatever[*].foo", want: TList(TAny())},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, c.src), TUnknown(), splatScope(), errs)
			assert.True(t, got.Equal(c.want), "got %s, want %s", got, c.want)
			assert.Empty(t, errs.Errors())
		})
	}
}

func TestInferSplatRejectsNonList(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "map", src: "var.m[*]", want: "splat [*] needs a list, got map(string)"},
		{name: "scalar field", src: "var.subnets[*].id[*]", want: "splat [*] needs a list, got string"},
		{
			name: "double splat on int list",
			src:  "var.nums[*][*]",
			want: "splat [*] needs a list, got integer",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, c.src), TUnknown(), splatScope(), errs)
			require.Len(t, errs.Errors(), 1)
			require.Equal(t, c.want, errs.Errors()[0].Msg)
		})
	}
}
