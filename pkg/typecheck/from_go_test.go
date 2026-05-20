package typecheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromGoString(t *testing.T) {
	tests := []struct {
		in   string
		want Type
	}{
		{"string", TString()},
		{"bool", TBoolean()},
		{"int", TInteger()},
		{"int64", TInteger()},
		{"uint32", TInteger()},
		{"float64", TNumber()},
		{"time.Duration", TInteger()},
		{"any", TAny()},
		{"interface{}", TAny()},
		{"*int64", TOptional(TInteger())},
		{"[]string", TList(TString())},
		{"[][]string", TList(TList(TString()))},
		{"map[string]string", TMap(TString())},
		{"map[string][]int64", TMap(TList(TInteger()))},
		{"map[int]string", TUnknown()},
		{"foo.Bar", TUnknown()},
		{"", TUnknown()},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := FromGoString(tt.in)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
		})
	}
}

func TestFromGoStringPointerOfMap(t *testing.T) {
	got := FromGoString("*map[string]string")
	want := TOptional(TMap(TString()))
	assert.True(t, got.Equal(want))
}
