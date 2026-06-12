package ub

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Tag
	}{
		{name: "empty value", value: "", want: Tag{}},
		{name: "name only", value: "bucket-name", want: Tag{Name: "bucket-name"}},
		{name: "skip", value: "-", want: Tag{Skip: true}},
		{
			name:  "dash with options is a name, not a skip",
			value: "-,omitempty",
			want:  Tag{Name: "-", Omitempty: true},
		},
		{name: "option without a name", value: ",omitempty", want: Tag{Omitempty: true}},
		{name: "omitempty", value: "x,omitempty", want: Tag{Name: "x", Omitempty: true}},
		{name: "sensitive", value: "x,sensitive", want: Tag{Name: "x", Sensitive: true}},
		{name: "squash is recognized and kept quiet", value: "x,squash", want: Tag{Name: "x"}},
		{
			name:  "every recognized option together",
			value: "x,omitempty,sensitive,squash",
			want:  Tag{Name: "x", Omitempty: true, Sensitive: true},
		},
		{
			name:  "unknown option is collected",
			value: "x,sensitiv",
			want:  Tag{Name: "x", Unknown: []string{"sensitiv"}},
		},
		{
			name:  "unknown options keep declaration order",
			value: "x,later,earlier",
			want:  Tag{Name: "x", Unknown: []string{"later", "earlier"}},
		},
		{
			name:  "spaces around the name and options are trimmed",
			value: " x , omitempty ",
			want:  Tag{Name: "x", Omitempty: true},
		},
		{name: "blank name trims to empty", value: " ", want: Tag{}},
		{name: "trailing comma is ignored", value: "x,", want: Tag{Name: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ParseTag(tt.value))
		})
	}
}

func TestTagFieldName(t *testing.T) {
	require.Equal(t, "explicit", Tag{Name: "explicit"}.FieldName("BucketName"))
	require.Equal(t, "bucket-name", Tag{}.FieldName("BucketName"))
}
