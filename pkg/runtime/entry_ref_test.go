package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntryRefValid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		address string
	}{
		{
			name:    "resource",
			input:   "resource.web",
			address: "resource.web",
		},
		{
			name:    "action",
			input:   "action.hi",
			address: "action.hi",
		},
		{
			name:    "data source",
			input:   "data-source.image",
			address: "data-source.image",
		},
		{
			name:    "composite child",
			input:   "resource.app/resource.sg",
			address: "resource.app/resource.sg",
		},
		{
			name:    "instance key",
			input:   "resource.subnets['old']",
			address: "resource.subnets['old']",
		},
		{
			name:    "instance key with slash and at sign",
			input:   "action.run['has/@sign']",
			address: "action.run['has/@sign']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEntryRef(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.address, got.Address)
			assert.Equal(t, tt.input, got.String())
		})
	}
}

func TestParseEntryRefInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "legacy qualified ref", in: "aws.instance@resource.web", want: "expected state ref"},
		{name: "empty address", in: "", want: "expected state ref"},
		{
			name: "data root",
			in:   "data.image",
			want: "address root must be resource, data-source, or action",
		},
		{
			name: "invalid root",
			in:   "input.web",
			want: "address root must be resource, data-source, or action",
		},
		{
			name: "address missing name",
			in:   "resource",
			want: "address segment must be <category>.<name>",
		},
		{
			name: "field access",
			in:   "resource.web.id",
			want: "state refs do not include field access",
		},
		{
			name: "malformed instance key",
			in:   "resource.web['old'",
			want: "malformed instance key",
		},
		{
			name: "unquoted instance key",
			in:   "resource.web[old]",
			want: "malformed instance key",
		},
		{
			name: "computed instance key",
			in:   "resource.web[input.name]",
			want: "malformed instance key",
		},
		{
			name: "splat instance key",
			in:   "resource.web[*]",
			want: "malformed instance key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEntryRef(tt.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestInstanceAddressEscapesKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "plain",
			key:  "blue",
			want: "resource.items['blue']",
		},
		{
			name: "quote",
			key:  "team's blue",
			want: "resource.items['team\\'s blue']",
		},
		{
			name: "backslash",
			key:  `a\b`,
			want: "resource.items['a\\\\b']",
		},
		{
			name: "slash",
			key:  "a/b",
			want: "resource.items['a/b']",
		},
		{
			name: "quote bracket",
			key:  "a']b",
			want: "resource.items['a\\']b']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := instanceAddress("resource.items", tt.key)
			assert.Equal(t, tt.want, got)

			template, key := SplitInstanceAddress(got)
			assert.Equal(t, "resource.items", template)
			assert.Equal(t, tt.key, key)
			assert.Equal(t, "resource.items", templateAddress(got))
		})
	}
}

func TestDirectParentIgnoresSlashInKey(t *testing.T) {
	root := instanceAddress("resource.items", "a/b")
	child := root + "/resource.member"

	assert.Equal(t, "", DirectParent(root))
	assert.Equal(t, root, DirectParent(child))
}

func TestEntryRefFromEntryAndNode(t *testing.T) {
	ent := &state.Entry{
		Address:  "resource.web",
		Selector: &state.Selector{Alias: "aws", Export: "instance"},
	}
	fromEntry, ok := EntryRefFromEntry(ent)
	require.True(t, ok)
	assert.Equal(t, "resource.web", fromEntry.String())

	node := &Node{Address: "resource.web", Alias: "aws", Type: "instance"}
	fromNode, ok := EntryRefFromNode(node)
	require.True(t, ok)
	assert.Equal(t, fromEntry, fromNode)
	assert.True(t, SameEntryRef(fromEntry, fromNode))

	_, ok = EntryRefFromEntry(&state.Entry{Address: "input.web"})
	assert.False(t, ok)
	_, ok = EntryRefFromNode(&Node{Address: "input.web"})
	assert.False(t, ok)
}
