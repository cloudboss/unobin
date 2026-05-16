package cfg

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubValidator struct {
	err  error
	seen []any
}

func (s *stubValidator) Check(v any) error {
	s.seen = append(s.seen, v)
	return s.err
}

func (s *stubValidator) Describe() ValidatorDesc {
	return ValidatorDesc{Kind: "stub"}
}

func TestDecodeAtomicFields(t *testing.T) {
	type Configuration struct {
		Region   String
		Profile  *String
		Replicas Integer
		Ratio    Number
		Enabled  Boolean
	}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{
				Profile: &String{Default: "default"},
			}
		},
	}
	raw := map[string]any{
		"region":   "us-east-1",
		"replicas": int64(5),
		"ratio":    1.5,
		"enabled":  true,
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	cfg := out.(*Configuration)
	require.Equal(t, "us-east-1", cfg.Region.Value)
	require.Equal(t, "default", cfg.Profile.Value)
	require.EqualValues(t, 5, cfg.Replicas.Value)
	require.InDelta(t, 1.5, cfg.Ratio.Value, 0.001)
	require.True(t, cfg.Enabled.Value)
}

func TestDecodeOptionalUsesValueWhenPresent(t *testing.T) {
	type Configuration struct {
		Profile *String
	}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{Profile: &String{Default: "default"}}
		},
	}
	out, err := Decode(ct, map[string]any{"profile": "prod"})
	require.NoError(t, err)
	require.Equal(t, "prod", out.(*Configuration).Profile.Value)
}

func TestDecodeRequiredFieldAbsentIsAnError(t *testing.T) {
	type Configuration struct {
		Region String
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
	require.Contains(t, err.Error(), "required")
}

func TestDecodeTypeMismatchIsAnError(t *testing.T) {
	type Configuration struct {
		Replicas Integer
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{"replicas": "five"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "replicas")
	require.Contains(t, err.Error(), "expected integer")
}

func TestDecodeUnknownKeyIsAnError(t *testing.T) {
	type Configuration struct {
		Region String
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{"region": "x", "rgeion": "typo"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rgeion")
}

func TestDecodeNestedStructRecurses(t *testing.T) {
	type AssumeRole struct {
		RoleARN     String
		SessionName *String
	}
	type Configuration struct {
		Region     String
		AssumeRole AssumeRole
	}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{
				AssumeRole: AssumeRole{
					SessionName: &String{Default: "unobin"},
				},
			}
		},
	}
	raw := map[string]any{
		"region": "us-east-1",
		"assume-role": map[string]any{
			"role-arn": "arn:aws:iam::1:role/foo",
		},
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	cfg := out.(*Configuration)
	require.Equal(t, "arn:aws:iam::1:role/foo", cfg.AssumeRole.RoleARN.Value)
	require.Equal(t, "unobin", cfg.AssumeRole.SessionName.Value)
}

func TestDecodeOptionalStructAbsentLeavesNil(t *testing.T) {
	type AssumeRole struct {
		RoleARN String
	}
	type Configuration struct {
		Region     String
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	out, err := Decode(ct, map[string]any{"region": "us-east-1"})
	require.NoError(t, err)
	require.Nil(t, out.(*Configuration).AssumeRole)
}

func TestDecodeOptionalStructPresentAllocates(t *testing.T) {
	type AssumeRole struct {
		RoleARN String
	}
	type Configuration struct {
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{
		"assume-role": map[string]any{
			"role-arn": "arn:aws:iam::1:role/foo",
		},
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	role := out.(*Configuration).AssumeRole
	require.NotNil(t, role)
	require.Equal(t, "arn:aws:iam::1:role/foo", role.RoleARN.Value)
}

func TestDecodeAllocatesNilWrapperPointer(t *testing.T) {
	type Configuration struct {
		Profile *String
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	out, err := Decode(ct, map[string]any{"profile": "prod"})
	require.NoError(t, err)
	require.NotNil(t, out.(*Configuration).Profile)
	require.Equal(t, "prod", out.(*Configuration).Profile.Value)
}

func TestDecodeStructMustBeMap(t *testing.T) {
	type AssumeRole struct {
		RoleARN String
	}
	type Configuration struct {
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{"assume-role": "not a map"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected a map")
}

func TestDecodeRunsValidatorOnDecodedValue(t *testing.T) {
	type Configuration struct {
		Region String
	}
	stub := &stubValidator{}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{Region: String{Validate: stub}}
		},
	}
	_, err := Decode(ct, map[string]any{"region": "us-east-1"})
	require.NoError(t, err)
	require.Equal(t, []any{"us-east-1"}, stub.seen)
}

func TestDecodeReportsValidatorFailureWithFieldPath(t *testing.T) {
	type Configuration struct {
		Region String
	}
	stub := &stubValidator{err: errors.New("bad region")}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{Region: String{Validate: stub}}
		},
	}
	_, err := Decode(ct, map[string]any{"region": "us-east-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
	require.Contains(t, err.Error(), "bad region")
}

func TestDecodeValidatesDefaultsToo(t *testing.T) {
	type Configuration struct {
		Profile *String
	}
	stub := &stubValidator{err: errors.New("default rejected")}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{
				Profile: &String{Default: "bad-default", Validate: stub},
			}
		},
	}
	_, err := Decode(ct, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "profile")
	require.Contains(t, err.Error(), "default rejected")
}

func TestDecodeSkipsValidatorOnTypeMismatch(t *testing.T) {
	type Configuration struct {
		Replicas Integer
	}
	stub := &stubValidator{}
	ct := &ConfigurationType{
		New: func() any {
			return &Configuration{Replicas: Integer{Validate: stub}}
		},
	}
	_, err := Decode(ct, map[string]any{"replicas": "five"})
	require.Error(t, err)
	require.Empty(t, stub.seen, "validator should not run when decode failed")
}
