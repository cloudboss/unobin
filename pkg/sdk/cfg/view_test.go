package cfg

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type viewAssumeRole struct {
	RoleArn    String
	ExternalID *String
}

type viewConfiguration struct {
	Region     String
	Profile    *String
	Enabled    *Boolean
	Retries    *Integer
	Ratio      *Number
	Tags       Map[String]
	Subnets    *List[String]
	AssumeRole *viewAssumeRole
}

func TestViewBuildsFieldsDefaultsAndDigest(t *testing.T) {
	ct := &ConfigurationType[*viewConfiguration]{
		New: func() *viewConfiguration {
			return &viewConfiguration{
				Profile: &String{Default: "default"},
				Enabled: &Boolean{Default: false},
				Retries: &Integer{Default: 3},
				Ratio:   &Number{Default: 0.5},
				Subnets: &List[String]{
					Default: []String{{Value: "subnet-a"}},
				},
			}
		},
	}

	got, err := View(ct)
	require.NoError(t, err)

	assumeRole := typecheck.TObject([]typecheck.ObjectField{
		{Name: "role-arn", Type: typecheck.TString()},
		{Name: "external-id", Type: typecheck.TString(), Optional: true},
	})
	wantFields := []typecheck.ObjectField{
		{Name: "region", Type: typecheck.TString()},
		{Name: "profile", Type: typecheck.TString(), Optional: true, Defaulted: true},
		{Name: "enabled", Type: typecheck.TBoolean(), Optional: true, Defaulted: true},
		{Name: "retries", Type: typecheck.TInteger(), Optional: true, Defaulted: true},
		{Name: "ratio", Type: typecheck.TNumber(), Optional: true, Defaulted: true},
		{Name: "tags", Type: typecheck.TMap(typecheck.TString())},
		{Name: "subnets", Type: typecheck.TList(typecheck.TString()), Optional: true, Defaulted: true},
		{Name: "assume-role", Type: assumeRole, Optional: true},
	}
	wantDefaults := []lang.DefaultSpec{
		{Field: "var.profile", Value: "'default'"},
		{Field: "var.enabled", Value: "false"},
		{Field: "var.retries", Value: "3"},
		{Field: "var.ratio", Value: "0.5"},
		{Field: "var.subnets", Value: "['subnet-a']"},
	}
	require.Equal(t, wantFields, got.Fields)
	require.Equal(t, wantDefaults, got.Defaults)
	require.False(t, got.Empty)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{64}$`), got.SchemaDigest)

	again, err := View(ct)
	require.NoError(t, err)
	require.Equal(t, got.SchemaDigest, again.SchemaDigest)
}

type viewRequiredDefault struct {
	Profile String
}

func TestViewIgnoresDefaultOnRequiredWrapper(t *testing.T) {
	ct := &ConfigurationType[*viewRequiredDefault]{
		New: func() *viewRequiredDefault {
			return &viewRequiredDefault{Profile: String{Default: "default"}}
		},
	}

	got, err := View(ct)
	require.NoError(t, err)
	require.Equal(t, []typecheck.ObjectField{
		{Name: "profile", Type: typecheck.TString()},
	}, got.Fields)
	require.Empty(t, got.Defaults)
}

func TestViewEmptyConfigHasDigest(t *testing.T) {
	nilView, err := View(nil)
	require.NoError(t, err)
	require.False(t, nilView.Empty)
	require.Empty(t, nilView.SchemaDigest)

	type empty struct{}
	emptyView, err := View(&ConfigurationType[*empty]{New: func() *empty { return &empty{} }})
	require.NoError(t, err)
	require.True(t, emptyView.Empty)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{64}$`), emptyView.SchemaDigest)
}
