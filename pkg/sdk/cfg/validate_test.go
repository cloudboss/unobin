package cfg

import (
	"testing"
	"time"

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
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeAcceptsPlainFields(t *testing.T) {
	type AssumeRole struct {
		RoleARN    string
		ExternalID *string
	}
	type Configuration struct {
		Region     string
		Enabled    bool
		Retries    int64
		Ratio      float64
		Opaque     any
		Timeout    time.Duration
		Subnets    []string
		Tags       map[string]string
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType[*Configuration]{
		New: func() *Configuration { return &Configuration{} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeRejectsUnsupportedPlainFields(t *testing.T) {
	type Configuration struct {
		Channel chan string
		Lookup  map[int]string
	}
	ct := &ConfigurationType[*Configuration]{
		New: func() *Configuration { return &Configuration{} },
	}
	err := ValidateConfigurationType(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Channel")
	require.Contains(t, err.Error(), "Lookup")
}

func TestValidateConfigurationTypeRejectsAnonymousField(t *testing.T) {
	type Embedded struct {
		Region string
	}
	type Configuration struct {
		Embedded
	}
	ct := &ConfigurationType[*Configuration]{
		New: func() *Configuration { return &Configuration{} },
	}
	err := ValidateConfigurationType(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "anonymous field")
}

func TestValidateConfigurationTypeSkipsUnexportedFields(t *testing.T) {
	type Configuration struct {
		Region String
		secret string
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{secret: "123"} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeTolerantOfRecursiveTypes(t *testing.T) {
	type Tree struct {
		Name  String
		Child *Tree
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Tree{} },
	}
	require.NoError(t, ValidateConfigurationType(ct))
}

func TestValidateConfigurationTypeRejectsNilOrEmpty(t *testing.T) {
	require.Error(t, ValidateConfigurationType(nil))
	require.Error(t, ValidateConfigurationType(&ConfigurationType[any]{}))
}

func TestValidateConfigurationTypeRejectsNonPointerNonStructResult(t *testing.T) {
	notAStruct := &ConfigurationType[any]{
		New: func() any {
			s := "not a struct"
			return &s
		},
	}
	require.Error(t, ValidateConfigurationType(notAStruct))

	notAPointer := &ConfigurationType[any]{
		New: func() any {
			return struct{ Region String }{}
		},
	}
	require.Error(t, ValidateConfigurationType(notAPointer))
}
