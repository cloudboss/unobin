package cfg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type describedNested struct {
	Host String
}

type describedConfiguration struct {
	Region     String
	Profile    *String
	Retries    Integer
	Ratio      *Number
	Verbose    Boolean
	Tags       Map[String]
	Subnets    List[String]
	Endpoint   Object[describedNested]
	AssumeRole *describedNested
}

func TestDescribeListsConfigurationFields(t *testing.T) {
	ct := &ConfigurationType{
		New: func() any {
			return &describedConfiguration{
				Region:  String{Description: "AWS region."},
				Profile: &String{Description: "Shared config profile."},
			}
		},
	}
	nested := []Field{{Name: "host", Type: "string"}}
	want := []Field{
		{Name: "region", Type: "string", Description: "AWS region."},
		{Name: "profile", Type: "string", Optional: true, Description: "Shared config profile."},
		{Name: "retries", Type: "integer"},
		{Name: "ratio", Type: "number", Optional: true},
		{Name: "verbose", Type: "boolean"},
		{Name: "tags", Type: "map(string)"},
		{Name: "subnets", Type: "list(string)"},
		{Name: "endpoint", Type: "object", Fields: nested},
		{Name: "assume-role", Type: "object", Optional: true, Fields: nested},
	}
	require.Equal(t, want, Describe(ct))
}

type describedLeaf struct {
	Arn String
}

type describedMiddle struct {
	Leaf  describedLeaf
	Label *String
}

func TestDescribeExpandsNestedObjects(t *testing.T) {
	type root struct {
		Middle *describedMiddle
	}
	ct := &ConfigurationType{New: func() any { return &root{} }}
	want := []Field{{
		Name: "middle", Type: "object", Optional: true,
		Fields: []Field{
			{Name: "leaf", Type: "object", Fields: []Field{{Name: "arn", Type: "string"}}},
			{Name: "label", Type: "string", Optional: true},
		},
	}}
	require.Equal(t, want, Describe(ct))
}

type describedSelf struct {
	Name  String
	Child *describedSelf
}

func TestDescribeStopsOnSelfReference(t *testing.T) {
	ct := &ConfigurationType{New: func() any { return &describedSelf{} }}
	want := []Field{
		{Name: "name", Type: "string"},
		{Name: "child", Type: "object", Optional: true},
	}
	require.Equal(t, want, Describe(ct))
}

func TestDescribeNilConfiguration(t *testing.T) {
	require.Nil(t, Describe(nil))
	require.Nil(t, Describe(&ConfigurationType{New: func() any { return nil }}))
}

func TestDescribeSkipsAnonymousField(t *testing.T) {
	ct := &ConfigurationType{New: func() any { return &hostWithEmbedded{} }}
	fields := Describe(ct)
	require.Len(t, fields, 1)
	require.Equal(t, "name", fields[0].Name)
}
