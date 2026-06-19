package typecheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func obj(fields ...ObjectField) Type { return TObject(fields) }

func req(name string, t Type) ObjectField {
	return ObjectField{Name: name, Type: t}
}

func opt(name string, t Type) ObjectField {
	return ObjectField{Name: name, Type: t, Optional: true}
}

func TestMergeShallow(t *testing.T) {
	tests := []struct {
		name string
		args []Type
		want Type
	}{
		{
			name: "no arguments",
			args: nil,
			want: obj(),
		},
		{
			name: "one object passes through",
			args: []Type{obj(req("a", TString()), opt("b", TInteger()))},
			want: obj(req("a", TString()), opt("b", TInteger())),
		},
		{
			name: "library config reads as object",
			args: []Type{
				TLibraryConfig("github.com/acme/aws", "github.com/acme/aws", "abc",
					[]ObjectField{req("region", TString())}),
				obj(req("profile", TString())),
			},
			want: obj(req("region", TString()), req("profile", TString())),
		},
		{
			name: "empty object adds nothing",
			args: []Type{obj(req("a", TString())), obj()},
			want: obj(req("a", TString())),
		},
		{
			name: "disjoint fields union",
			args: []Type{obj(req("a", TString())), obj(req("b", TInteger()))},
			want: obj(req("a", TString()), req("b", TInteger())),
		},
		{
			name: "later required field replaces type",
			args: []Type{obj(req("a", TString())), obj(req("a", TInteger()))},
			want: obj(req("a", TInteger())),
		},
		{
			name: "three argument chain keeps last",
			args: []Type{
				obj(req("a", TString()), req("b", TString())),
				obj(req("b", TInteger())),
				obj(req("a", TBoolean())),
			},
			want: obj(req("a", TBoolean()), req("b", TInteger())),
		},
		{
			name: "later optional field joins as union",
			args: []Type{obj(req("a", TString())), obj(opt("a", TInteger()))},
			want: obj(req("a", TUnion([]Type{TString(), TInteger()}))),
		},
		{
			name: "later optional field of same type folds",
			args: []Type{obj(req("a", TString())), obj(opt("a", TString()))},
			want: obj(req("a", TString())),
		},
		{
			name: "optional field with no earlier stays optional",
			args: []Type{obj(req("a", TString())), obj(opt("b", TInteger()))},
			want: obj(req("a", TString()), opt("b", TInteger())),
		},
		{
			name: "optional then required becomes required",
			args: []Type{obj(opt("a", TString())), obj(req("a", TInteger()))},
			want: obj(req("a", TInteger())),
		},
		{
			name: "required then optional then required",
			args: []Type{
				obj(req("a", TString())),
				obj(opt("a", TInteger())),
				obj(req("a", TBoolean())),
			},
			want: obj(req("a", TBoolean())),
		},
		{
			name: "union accumulates distinct optional types",
			args: []Type{
				obj(req("a", TString())),
				obj(opt("a", TInteger())),
				obj(opt("a", TBoolean())),
			},
			want: obj(req("a", TUnion([]Type{TString(), TInteger(), TBoolean()}))),
		},
		{
			name: "possibly null argument joins all its fields",
			args: []Type{
				obj(req("a", TString()), req("b", TString())),
				TOptional(obj(req("a", TInteger()), req("c", TBoolean()))),
			},
			want: obj(
				req("a", TUnion([]Type{TString(), TInteger()})),
				req("b", TString()),
				opt("c", TBoolean()),
			),
		},
		{
			name: "possibly null first argument leaves fields optional",
			args: []Type{TOptional(obj(req("a", TString()))), obj(req("b", TInteger()))},
			want: obj(opt("a", TString()), req("b", TInteger())),
		},
		{
			name: "null argument adds nothing",
			args: []Type{TNull(), obj(req("a", TString())), TNull()},
			want: obj(req("a", TString())),
		},
		{
			name: "nested object field replaced wholesale",
			args: []Type{
				obj(req("a", obj(req("x", TString()), req("y", TString())))),
				obj(req("a", obj(req("z", TInteger())))),
			},
			want: obj(req("a", obj(req("z", TInteger())))),
		},
		{
			name: "list field replaced wholesale",
			args: []Type{obj(req("a", TList(TInteger())))},
			want: obj(req("a", TList(TInteger()))),
		},
		{
			name: "map argument loses precision",
			args: []Type{obj(req("a", TString())), TMap(TString())},
			want: TUnknown(),
		},
		{
			name: "opaque argument loses precision",
			args: []Type{obj(req("a", TString())), TOpaque()},
			want: TUnknown(),
		},
		{
			name: "unknown argument loses precision",
			args: []Type{TUnknown(), obj(req("a", TString()))},
			want: TUnknown(),
		},
		{
			name: "possibly null map loses precision",
			args: []Type{TOptional(TMap(TString()))},
			want: TUnknown(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeShallow(tt.args)
			assert.True(t, tt.want.Equal(got), "want %s, got %s", tt.want, got)
		})
	}

	t.Run("deterministic", func(t *testing.T) {
		for _, tt := range tests {
			first := MergeShallow(tt.args).String()
			for range 5 {
				assert.Equal(t, first, MergeShallow(tt.args).String(), tt.name)
			}
		}
	})
}
