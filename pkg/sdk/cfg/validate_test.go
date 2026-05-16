package cfg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfigurationTypeAcceptsValidConfig(t *testing.T) {
	type AssumeRole struct {
		RoleARN     String
		SessionName *String
		Duration    *Integer
	}
	type Configuration struct {
		Region     String
		Profile    *String
		Subnets    List[String]
		Tags       Map[String]
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeRejectsBareGoType(t *testing.T) {
	type Configuration struct {
		Region string
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	err := ValidateConfigurationType(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Region")
	require.Contains(t, err.Error(), "string")
}

func TestValidateConfigurationTypeRejectsBareGoTypeInsideNestedStruct(t *testing.T) {
	type AssumeRole struct {
		RoleARN  String
		Duration int
	}
	type Configuration struct {
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	err := ValidateConfigurationType(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "AssumeRole.Duration")
}

func TestValidateConfigurationTypeRejectsNakedSliceOrMap(t *testing.T) {
	type Configuration struct {
		Hosts []string
		Tags  map[string]string
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	err := ValidateConfigurationType(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Hosts")
	require.Contains(t, err.Error(), "Tags")
}

func TestValidateConfigurationTypeSkipsUnexportedFields(t *testing.T) {
	type Configuration struct {
		Region String
		secret string
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{secret: "123"} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeTolerantOfRecursiveTypes(t *testing.T) {
	type Tree struct {
		Name  String
		Child *Tree
	}
	ct := &ConfigurationType{
		New: func() any { return &Tree{} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeRejectsNilOrEmpty(t *testing.T) {
	require.Error(t, ValidateConfigurationType(nil))
	require.Error(t, ValidateConfigurationType(&ConfigurationType{}))
}

func TestValidateConfigurationTypeRejectsNonPointerNonStructResult(t *testing.T) {
	notAStruct := &ConfigurationType{
		New: func() any {
			s := "not a struct"
			return &s
		},
	}
	require.Error(t, ValidateConfigurationType(notAStruct))

	notAPointer := &ConfigurationType{
		New: func() any {
			return struct{ Region String }{}
		},
	}
	require.Error(t, ValidateConfigurationType(notAPointer))
}
