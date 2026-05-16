package validate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func TestPatternAcceptsMatchingString(t *testing.T) {
	v := Pattern("^[a-z]+$")
	require.NoError(t, v.Check("hello"))
}

func TestPatternRejectsNonMatchingString(t *testing.T) {
	v := Pattern("^[a-z]+$")
	err := v.Check("Hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match")
}

func TestPatternRejectsNonString(t *testing.T) {
	v := Pattern("^.*$")
	err := v.Check(42)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires a string")
}

func TestPatternDescribesItself(t *testing.T) {
	d := Pattern("^x+$").Describe()
	require.Equal(t, "pattern", d.Kind)
	require.Equal(t, "^x+$", d.Params["pattern"])
}

func TestRangeAcceptsInBounds(t *testing.T) {
	v := Range(1, 65535)
	require.NoError(t, v.Check(int64(80)))
	require.NoError(t, v.Check(int64(1)))
	require.NoError(t, v.Check(int64(65535)))
}

func TestRangeRejectsOutOfBounds(t *testing.T) {
	v := Range(1, 100)
	err := v.Check(int64(101))
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside range")
}

func TestRangeRejectsNonInteger(t *testing.T) {
	v := Range(1, 100)
	err := v.Check("fifty")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an integer")
}

func TestRangeDescribesItself(t *testing.T) {
	d := Range(1, 65535).Describe()
	require.Equal(t, "range", d.Kind)
	require.EqualValues(t, 1, d.Params["min"])
	require.EqualValues(t, 65535, d.Params["max"])
}

func TestAllRunsEveryChildUntilSuccess(t *testing.T) {
	v := All(
		Pattern("^[a-z]+$"),
		Func("max length 5", func(x any) error {
			if len(x.(string)) > 5 {
				return errors.New("too long")
			}
			return nil
		}),
	)
	require.NoError(t, v.Check("hello"))
}

func TestAllStopsAtFirstFailure(t *testing.T) {
	var second bool
	v := All(
		Pattern("^[a-z]+$"),
		Func("second", func(any) error {
			second = true
			return nil
		}),
	)
	err := v.Check("Hello")
	require.Error(t, err)
	require.False(t, second, "second child must not run after the first fails")
}

func TestAllDescribesItselfWithChildren(t *testing.T) {
	d := All(Pattern("^x$"), Range(1, 9)).Describe()
	require.Equal(t, "all", d.Kind)
	children, ok := d.Params["children"].([]cfg.ValidatorDesc)
	require.True(t, ok)
	require.Len(t, children, 2)
	require.Equal(t, "pattern", children[0].Kind)
	require.Equal(t, "range", children[1].Kind)
}

func TestFuncCallsTheBackingFunction(t *testing.T) {
	v := Func("must be positive", func(x any) error {
		n := x.(int64)
		if n <= 0 {
			return errors.New("not positive")
		}
		return nil
	})
	require.NoError(t, v.Check(int64(5)))
	require.Error(t, v.Check(int64(-1)))
}

func TestFuncDescribesItselfAsCustom(t *testing.T) {
	d := Func("does a thing", func(any) error { return nil }).Describe()
	require.Equal(t, "custom", d.Kind)
	require.Equal(t, "does a thing", d.Params["description"])
}
