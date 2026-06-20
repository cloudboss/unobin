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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
		New: func() any {
			return &Configuration{Profile: &String{Default: "default"}}
		},
	}
	out, err := Decode(ct, map[string]any{"profile": "prod"})
	require.NoError(t, err)
	require.Equal(t, "prod", out.(*Configuration).Profile.Value)
}

func TestDecodeOptionalUsesDefaultWhenNull(t *testing.T) {
	type Configuration struct {
		Profile *String
		Tags    *Map[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any {
			return &Configuration{
				Profile: &String{Default: "default"},
				Tags: &Map[String]{Default: map[string]String{
					"env": {Value: "dev"},
				}},
			}
		},
	}
	out, err := Decode(ct, map[string]any{"profile": nil, "tags": nil})
	require.NoError(t, err)
	config := out.(*Configuration)
	require.Equal(t, "default", config.Profile.Value)
	require.Equal(t, "dev", config.Tags.Value["env"].Value)
}

func TestDecodeOptionalStructNullLeavesNil(t *testing.T) {
	type AssumeRole struct {
		RoleARN String
	}
	type Configuration struct {
		Region     String
		AssumeRole *AssumeRole
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	out, err := Decode(ct, map[string]any{
		"region":      "us-east-1",
		"assume-role": nil,
	})
	require.NoError(t, err)
	config := out.(*Configuration)
	require.Equal(t, "us-east-1", config.Region.Value)
	require.Nil(t, config.AssumeRole)
}

func TestDecodeRequiredFieldAbsentIsAnError(t *testing.T) {
	type Configuration struct {
		Region String
	}
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{"replicas": "five"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "replicas")
	require.Contains(t, err.Error(), "expected an integer")
}

func TestDecodeTypeMismatchUsesUbTypeNames(t *testing.T) {
	type Inner struct {
		Host String
	}
	type Configuration struct {
		Name     String
		Replicas Integer
		Ratio    Number
		Enabled  Boolean
		Subnets  List[String]
		Tags     Map[String]
		Inner    Inner
		Server   Object[Inner]
	}
	tests := []struct {
		name    string
		raw     map[string]any
		wantSub string
	}{
		{
			name:    "string got integer",
			raw:     map[string]any{"name": int64(5)},
			wantSub: "name: expected a string, got an integer",
		},
		{
			name:    "integer got string",
			raw:     map[string]any{"name": "x", "replicas": "five"},
			wantSub: "replicas: expected an integer, got a string",
		},
		{
			name:    "number got boolean",
			raw:     map[string]any{"name": "x", "ratio": true},
			wantSub: "ratio: expected a number, got a boolean",
		},
		{
			name:    "boolean got null",
			raw:     map[string]any{"name": "x", "enabled": nil},
			wantSub: "enabled: expected a boolean, got null",
		},
		{
			name:    "list got object",
			raw:     map[string]any{"name": "x", "subnets": map[string]any{}},
			wantSub: "subnets: expected a list, got an object",
		},
		{
			name:    "map got list",
			raw:     map[string]any{"name": "x", "tags": []any{}},
			wantSub: "tags: expected a map, got a list",
		},
		{
			name:    "nested struct got string",
			raw:     map[string]any{"name": "x", "inner": "oops"},
			wantSub: "inner: expected a map, got a string",
		},
		{
			name:    "object got string",
			raw:     map[string]any{"name": "x", "server": "oops"},
			wantSub: "server: expected a map, got a string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ConfigurationType[any]{
				New: func() any { return &Configuration{} },
			}
			_, err := Decode(ct, tt.raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantSub)
		})
	}
}

func TestDecodeUnknownKeyIsAnError(t *testing.T) {
	type Configuration struct {
		Region String
	}
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
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
	ct := &ConfigurationType[any]{
		New: func() any {
			return &Configuration{Replicas: Integer{Validate: stub}}
		},
	}
	_, err := Decode(ct, map[string]any{"replicas": "five"})
	require.Error(t, err)
	require.Empty(t, stub.seen, "validator should not run when decode failed")
}

func TestDecodeObjectPopulatesInnerStruct(t *testing.T) {
	type Server struct {
		Host String
		Port Integer
	}
	type Configuration struct {
		Primary Object[Server]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{
		"primary": map[string]any{
			"host": "db.internal",
			"port": int64(5432),
		},
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	primary := out.(*Configuration).Primary
	require.Equal(t, "db.internal", primary.Value.Host.Value)
	require.EqualValues(t, 5432, primary.Value.Port.Value)
}

func TestDecodeObjectMissingRequiredErrors(t *testing.T) {
	type Server struct {
		Host String
	}
	type Configuration struct {
		Primary Object[Server]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "primary")
	require.Contains(t, err.Error(), "required")
}

func TestDecodeObjectOptionalAbsentLeavesZero(t *testing.T) {
	type Server struct {
		Host String
	}
	type Configuration struct {
		Primary *Object[Server]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	out, err := Decode(ct, map[string]any{})
	require.NoError(t, err)
	require.Nil(t, out.(*Configuration).Primary)
}

func TestDecodeObjectRunsValidatorOnInnerStruct(t *testing.T) {
	type Server struct {
		Host String
	}
	type Configuration struct {
		Primary Object[Server]
	}
	stub := &stubValidator{}
	ct := &ConfigurationType[any]{
		New: func() any {
			return &Configuration{
				Primary: Object[Server]{Validate: stub},
			}
		},
	}
	_, err := Decode(ct, map[string]any{
		"primary": map[string]any{"host": "x"},
	})
	require.NoError(t, err)
	require.Len(t, stub.seen, 1)
	got := stub.seen[0].(Server)
	require.Equal(t, "x", got.Host.Value)
}

func TestDecodeObjectInnerErrorPropagates(t *testing.T) {
	type Server struct {
		Host String
	}
	type Configuration struct {
		Primary Object[Server]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{
		"primary": map[string]any{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "primary.host")
}

func TestDecodeListOfStrings(t *testing.T) {
	type Configuration struct {
		Subnets List[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{"subnets": []any{"a", "b", "c"}}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	got := out.(*Configuration).Subnets.Value
	require.Len(t, got, 3)
	require.Equal(t, "a", got[0].Value)
	require.Equal(t, "c", got[2].Value)
}

func TestDecodeListOfIntegers(t *testing.T) {
	type Configuration struct {
		Ports List[Integer]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{"ports": []any{int64(80), int64(443)}}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	got := out.(*Configuration).Ports.Value
	require.Len(t, got, 2)
	require.EqualValues(t, 80, got[0].Value)
	require.EqualValues(t, 443, got[1].Value)
}

func TestDecodeListMustBeList(t *testing.T) {
	type Configuration struct {
		Subnets List[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{"subnets": "oops"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected a list")
}

func TestDecodeListRequiredAbsentErrors(t *testing.T) {
	type Configuration struct {
		Subnets List[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "subnets")
}

func TestDecodeListOptionalAbsentUsesDefault(t *testing.T) {
	type Configuration struct {
		Subnets *List[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any {
			return &Configuration{
				Subnets: &List[String]{
					Default: []String{{Value: "default-subnet"}},
				},
			}
		},
	}
	out, err := Decode(ct, map[string]any{})
	require.NoError(t, err)
	got := out.(*Configuration).Subnets.Value
	require.Len(t, got, 1)
	require.Equal(t, "default-subnet", got[0].Value)
}

func TestDecodeListElementValidatorRunsPerItem(t *testing.T) {
	type Configuration struct {
		Subnets List[String]
	}
	stub := &stubValidator{}
	ct := &ConfigurationType[any]{
		New: func() any {
			return &Configuration{
				Subnets: List[String]{Element: String{Validate: stub}},
			}
		},
	}
	_, err := Decode(ct, map[string]any{"subnets": []any{"a", "b"}})
	require.NoError(t, err)
	require.Equal(t, []any{"a", "b"}, stub.seen)
}

func TestDecodeListOfObjects(t *testing.T) {
	type Server struct {
		Host String
		Port Integer
	}
	type Configuration struct {
		Servers List[Object[Server]]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{
		"servers": []any{
			map[string]any{"host": "a", "port": int64(80)},
			map[string]any{"host": "b", "port": int64(81)},
		},
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	got := out.(*Configuration).Servers.Value
	require.Len(t, got, 2)
	require.Equal(t, "a", got[0].Value.Host.Value)
	require.EqualValues(t, 81, got[1].Value.Port.Value)
}

func TestDecodeMapOfStrings(t *testing.T) {
	type Configuration struct {
		Tags Map[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{
		"tags": map[string]any{"Name": "web", "Env": "prod"},
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	got := out.(*Configuration).Tags.Value
	require.Len(t, got, 2)
	require.Equal(t, "web", got["Name"].Value)
	require.Equal(t, "prod", got["Env"].Value)
}

func TestDecodeMapMustBeMap(t *testing.T) {
	type Configuration struct {
		Tags Map[String]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	_, err := Decode(ct, map[string]any{"tags": []any{"oops"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected a map")
}

func TestDecodeNestedMapOfMaps(t *testing.T) {
	type Configuration struct {
		PerRegion Map[Map[String]]
	}
	ct := &ConfigurationType[any]{
		New: func() any { return &Configuration{} },
	}
	raw := map[string]any{
		"per-region": map[string]any{
			"east": map[string]any{"profile": "prod"},
			"west": map[string]any{"profile": "dev"},
		},
	}
	out, err := Decode(ct, raw)
	require.NoError(t, err)
	regions := out.(*Configuration).PerRegion.Value
	require.Equal(t, "prod", regions["east"].Value["profile"].Value)
	require.Equal(t, "dev", regions["west"].Value["profile"].Value)
}

type EmbeddedPart struct {
	Region String
}

type hostWithEmbedded struct {
	EmbeddedPart
	Name String
}

func TestDecodeRejectsAnonymousField(t *testing.T) {
	ct := &ConfigurationType[any]{New: func() any { return &hostWithEmbedded{} }}
	_, err := Decode(ct, map[string]any{"name": "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "anonymous field")
	require.Contains(t, err.Error(), "use a named field")
}
