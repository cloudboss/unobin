package defaults

import "testing"

func TestNullableValueCompilesForPointers(t *testing.T) {
	var s *string
	var n *int64
	_ = NullableValue(s, "dev")
	_ = NullableValue(n, int64(3))
}
