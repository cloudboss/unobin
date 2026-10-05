package runtime

import "testing"

var result int

func BenchmarkWork(b *testing.B) {
	b.Run("n=10", func(b *testing.B) {
		values := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		if Sum(values) != 55 {
			b.Fatal("unexpected sum")
		}
		for b.Loop() {
			result = Sum(values)
		}
		b.ReportMetric(10, "nodes")
	})
}
