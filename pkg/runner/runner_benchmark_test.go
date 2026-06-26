package runner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func BenchmarkPrepareFactoryLarge(b *testing.B) {
	info := benchmarkInfo(largeFactorySource(1000))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		parsed, err := parseFactory(info)
		if err != nil {
			b.Fatal(err)
		}
		if parsed.dag == nil {
			b.Fatal("nil dag")
		}
	}
}

func benchmarkInfo(src string) Info {
	coreMod := &runtime.Library{
		Name: "core",
		Actions: map[string]runtime.ActionRegistration{
			"echo": runtime.MakeAction[echoAction, any, any](),
		},
	}
	return Info{
		FactoryName:     "benchmark-stack",
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		FactoryBody:     src,
		Libraries:       map[string]*runtime.Library{"core": coreMod},
	}
}

func largeFactorySource(nodes int) string {
	var b strings.Builder
	b.WriteString(ubBenchmarkHeader("factory"))
	b.WriteString(" {\n")
	b.WriteString("  ")
	b.WriteString(ubBenchmarkHeader("imports"))
	b.WriteString(" { core: 'example.com/core' }\n\n")
	b.WriteString("  ")
	b.WriteString(ubBenchmarkHeader("actions"))
	b.WriteString(" {\n")
	for i := range nodes {
		fmt.Fprintf(&b, "    step-%04d: core.echo { echo: 'x' }\n", i)
	}
	b.WriteString("  }\n\n")
	b.WriteString("  ")
	b.WriteString(ubBenchmarkHeader("outputs"))
	b.WriteString(" {\n")
	for _, i := range benchmarkOutputIndexes(nodes) {
		fmt.Fprintf(&b, "    step-%04d: { value: action.step-%04d.echo }\n", i, i)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func benchmarkOutputIndexes(nodes int) []int {
	if nodes <= 1 {
		return []int{0}
	}
	return []int{0, nodes / 2, nodes - 1}
}

func ubBenchmarkHeader(name string) string {
	return name + ":"
}
