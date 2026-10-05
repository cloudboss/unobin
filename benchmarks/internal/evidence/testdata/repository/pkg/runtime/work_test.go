package runtime

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]int{2, 3, 5}); got != 10 {
		t.Fatalf("sum = %d, want 10", got)
	}
}
