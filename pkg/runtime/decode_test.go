package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sampleAction struct {
	Argv        []string
	Environment map[string]string
	Timeout     time.Duration
}

func TestDecodePopulatesFields(t *testing.T) {
	a := &sampleAction{}
	err := Decode(a, map[string]any{
		"argv":        []any{"echo", "hi"},
		"environment": map[string]any{"FOO": "bar"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"echo", "hi"}, a.Argv)
	require.Equal(t, map[string]string{"FOO": "bar"}, a.Environment)
}

type pointerReferenceInputs struct {
	Tags  *map[string]string
	Names *[]string
}

func TestDecodePopulatesPointerReferences(t *testing.T) {
	v := &pointerReferenceInputs{}
	err := Decode(v, map[string]any{
		"tags":  map[string]any{"env": "dev"},
		"names": []any{"a", "b"},
	})
	require.NoError(t, err)
	require.NotNil(t, v.Tags)
	require.NotNil(t, v.Names)
	require.Equal(t, map[string]string{"env": "dev"}, *v.Tags)
	require.Equal(t, []string{"a", "b"}, *v.Names)
}

func TestDecodeLeavesMissingPointerReferencesNil(t *testing.T) {
	v := &pointerReferenceInputs{}
	require.NoError(t, Decode(v, map[string]any{}))
	require.Nil(t, v.Tags)
	require.Nil(t, v.Names)
}

func TestDecodeLeavesNullPointerReferencesNil(t *testing.T) {
	v := &pointerReferenceInputs{}
	require.NoError(t, Decode(v, map[string]any{"tags": nil, "names": nil}))
	require.Nil(t, v.Tags)
	require.Nil(t, v.Names)
}

type pointerScalarInputs struct {
	Profile *string
}

func TestDecodeLeavesNullPointerScalarsNil(t *testing.T) {
	v := &pointerScalarInputs{}
	require.NoError(t, Decode(v, map[string]any{"profile": nil}))
	require.Nil(t, v.Profile)
}

func TestDecodeDurationFromString(t *testing.T) {
	a := &sampleAction{}
	err := Decode(a, map[string]any{"timeout": "250ms"})
	require.NoError(t, err)
	require.Equal(t, 250*time.Millisecond, a.Timeout)
}

func TestDecodeDurationFromNanos(t *testing.T) {
	a := &sampleAction{}
	err := Decode(a, map[string]any{"timeout": int64(500_000_000)})
	require.NoError(t, err)
	require.Equal(t, 500*time.Millisecond, a.Timeout)
}

func TestDecodeRejectsUnknownKey(t *testing.T) {
	a := &sampleAction{}
	err := Decode(a, map[string]any{"argv": []any{"x"}, "bogus": 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bogus")
}

func TestDecodeNilDestination(t *testing.T) {
	err := Decode(nil, map[string]any{"argv": []any{"x"}})
	require.Error(t, err)
}

func TestDecodeEmptyInputs(t *testing.T) {
	a := &sampleAction{}
	require.NoError(t, Decode(a, nil))
	require.NoError(t, Decode(a, map[string]any{}))
	require.Empty(t, a.Argv)
}

func TestDecodeWithActionRegistration(t *testing.T) {
	at := MakeAction[fakeAction, any, any]()
	a := at.NewReceiver()
	require.NoError(t, Decode(a, map[string]any{"echo": "hi"}))
	require.Equal(t, "hi", a.(*fakeAction).Echo)
}

type ubTagged struct {
	CidrBlock string `ub:"cidr-block"`
	AWSKMS    string `ub:"aws-kms"`
	Password  string `ub:",sensitive"`
	Untagged  string
}

func TestDecodeUsesUBTagName(t *testing.T) {
	v := &ubTagged{}
	err := Decode(v, map[string]any{"cidr-block": "10.0.0.0/16"})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", v.CidrBlock)
}

func TestDecodeExplicitUBNameForMergedAcronym(t *testing.T) {
	// AWSKMS kebabs to "awskms", so the explicit ub name is what lets
	// the field decode from the key "aws-kms".
	v := &ubTagged{}
	err := Decode(v, map[string]any{"aws-kms": "key-1"})
	require.NoError(t, err)
	require.Equal(t, "key-1", v.AWSKMS)
}

func TestDecodeKebabDefaultForUntaggedField(t *testing.T) {
	v := &ubTagged{}
	err := Decode(v, map[string]any{"untagged": "here"})
	require.NoError(t, err)
	require.Equal(t, "here", v.Untagged)
}

func TestDecodeIgnoresSensitiveOption(t *testing.T) {
	// The sensitive option is a compile-time signal; the decoder takes
	// the kebab default name and ignores the option.
	v := &ubTagged{}
	err := Decode(v, map[string]any{"password": "shh"})
	require.NoError(t, err)
	require.Equal(t, "shh", v.Password)
}

func TestDecodeRejectsNonStructDestination(t *testing.T) {
	var v string
	err := Decode(&v, map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pointer to a struct")
}
