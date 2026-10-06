package lang

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkExpressionTraversal(b *testing.B) {
	for _, fields := range []int{10, 100, 1000} {
		for _, method := range []string{"walk", "scan"} {
			b.Run(fmt.Sprintf("fields=%d/method=%s", fields, method), func(b *testing.B) {
				expression := largeExprScannerExpression(fields)
				visits := 0
				b.ReportAllocs()
				for b.Loop() {
					visits = 0
					if method == "walk" {
						Walk(expression, func(Expr) { visits++ })
					} else {
						ScanExpr(expression, ScanCallbacks{Expr: func(Expr, ScanContext) ScanDecision {
							visits++
							return ScanContinue
						}})
					}
				}
				require.Equal(b, 1+16*fields, visits)
				b.ReportMetric(float64(fields), "fields")
				b.ReportMetric(float64(visits), "expressions")
			})
		}
	}
}
