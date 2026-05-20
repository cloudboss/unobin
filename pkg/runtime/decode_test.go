package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sampleAction struct {
	Argv        []string          `mapstructure:"argv"`
	Environment map[string]string `mapstructure:"environment"`
	Timeout     time.Duration     `mapstructure:"timeout"`
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
	at := MakeAction[fakeAction, any]()
	a := at.NewReceiver()
	require.NoError(t, Decode(a, map[string]any{"echo": "hi"}))
	require.Equal(t, "hi", a.(*fakeAction).Echo)
}
