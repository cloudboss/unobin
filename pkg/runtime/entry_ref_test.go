package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntryRefValid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		selector state.Selector
		address  string
	}{
		{
			name:     "resource",
			input:    "aws.instance@resource.web",
			selector: state.Selector{Alias: "aws", Export: "instance"},
			address:  "resource.web",
		},
		{
			name:     "action",
			input:    "core.echo@action.hi",
			selector: state.Selector{Alias: "core", Export: "echo"},
			address:  "action.hi",
		},
		{
			name:     "data",
			input:    "aws.ami@data.image",
			selector: state.Selector{Alias: "aws", Export: "ami"},
			address:  "data.image",
		},
		{
			name:     "composite child",
			input:    "aws.security-group@resource.app/resource.sg",
			selector: state.Selector{Alias: "aws", Export: "security-group"},
			address:  "resource.app/resource.sg",
		},
		{
			name:     "instance key",
			input:    "aws.subnet@resource.subnets['old']",
			selector: state.Selector{Alias: "aws", Export: "subnet"},
			address:  "resource.subnets['old']",
		},
		{
			name:     "instance key with at sign",
			input:    "core.echo@action.run['has@sign']",
			selector: state.Selector{Alias: "core", Export: "echo"},
			address:  "action.run['has@sign']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEntryRef(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.selector, got.Selector)
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
		{name: "missing at sign", in: "resource.web", want: "expected <selector>@<address>"},
		{name: "empty selector", in: "@resource.web", want: "missing selector"},
		{name: "empty address", in: "aws.instance@", want: "missing address"},
		{name: "one selector segment", in: "aws@resource.web", want: "selector must have two segments"},
		{
			name: "three selector segments",
			in:   "aws.instance.extra@resource.web",
			want: "selector must have two segments",
		},
		{
			name: "empty selector segment",
			in:   "aws.@resource.web",
			want: "selector must have two segments",
		},
		{
			name: "invalid address root",
			in:   "aws.instance@var.web",
			want: "address root must be resource, data, or action",
		},
		{
			name: "address missing name",
			in:   "aws.instance@resource",
			want: "address must start with resource., data., or action",
		},
		{
			name: "malformed instance key",
			in:   "aws.instance@resource.web['old'",
			want: "malformed instance key",
		},
		{
			name: "unquoted instance key",
			in:   "aws.instance@resource.web[old]",
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

func TestEntryRefFromEntryAndNode(t *testing.T) {
	ent := &state.Entry{
		Address:  "resource.web",
		Selector: &state.Selector{Alias: "aws", Export: "instance"},
	}
	fromEntry, ok := EntryRefFromEntry(ent)
	require.True(t, ok)
	assert.Equal(t, "aws.instance@resource.web", fromEntry.String())

	node := &Node{Address: "resource.web", Alias: "aws", Type: "instance"}
	fromNode, ok := EntryRefFromNode(node)
	require.True(t, ok)
	assert.Equal(t, fromEntry, fromNode)
	assert.True(t, SameEntryRef(fromEntry, fromNode))

	_, ok = EntryRefFromEntry(&state.Entry{Address: "resource.web"})
	assert.False(t, ok)
	_, ok = EntryRefFromNode(&Node{Address: "resource.web"})
	assert.False(t, ok)
}
