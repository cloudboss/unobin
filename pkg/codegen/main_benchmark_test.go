package codegen

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkGenerateMainLargeFactory(b *testing.B) {
	in := Input{
		Body:        largeMainFactorySource(1000),
		FactoryName: "benchmark-stack",
		GoImports: map[string]string{
			"core": "example.com/core",
		},
	}

	var out []byte
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var err error
		out, err = Generate(in)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(len(out)), "bytes/output")
}

func largeMainFactorySource(nodes int) string {
	var b strings.Builder
	b.WriteString(mainBenchmarkHeader("factory"))
	b.WriteString(" {\n")
	b.WriteString("  ")
	b.WriteString(mainBenchmarkHeader("imports"))
	b.WriteString(" { core: 'example.com/core' }\n\n")
	b.WriteString("  ")
	b.WriteString(mainBenchmarkHeader("actions"))
	b.WriteString(" {\n")
	for i := range nodes {
		fmt.Fprintf(&b, "    step-%04d: core.echo { echo: 'x' }\n", i)
	}
	b.WriteString("  }\n\n")
	b.WriteString("  ")
	b.WriteString(mainBenchmarkHeader("outputs"))
	b.WriteString(" {\n")
	for _, i := range mainBenchmarkOutputIndexes(nodes) {
		fmt.Fprintf(&b, "    step-%04d: { value: action.step-%04d.echo }\n", i, i)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func mainBenchmarkOutputIndexes(nodes int) []int {
	if nodes <= 1 {
		return []int{0}
	}
	return []int{0, nodes / 2, nodes - 1}
}

func mainBenchmarkHeader(name string) string {
	return name + ":"
}
