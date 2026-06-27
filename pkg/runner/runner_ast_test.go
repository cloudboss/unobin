package runner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestParseFactoryRequiresBody(t *testing.T) {
	_, err := parseFactory(Info{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory body is required")
}

func TestParseFactoryUsesSyntaxBody(t *testing.T) {
	body := runnerSyntaxBody(t)
	info := Info{
		FactoryName:     "test-stack",
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		FactoryBody:     &body,
		Libraries: map[string]*runtime.Library{
			"core": {
				Name: "core",
				Actions: map[string]runtime.ActionRegistration{
					"echo": runtime.MakeAction[echoAction, any, any](),
				},
			},
		},
	}

	parsed, err := parseFactory(info)
	require.NoError(t, err)
	require.Same(t, &body, parsed.syntaxBody)
	require.Contains(t, parsed.dag.Nodes, "action.say")
}

func runnerSyntaxBody(t *testing.T) syntax.FactoryBody {
	t.Helper()
	sf, err := syntax.ParseSource("factory.ub", []byte(runnerSyntaxSource()))
	require.NoError(t, err)
	require.NotNil(t, sf.Factory)
	return sf.Factory.Body
}

func runnerSyntaxSource() string {
	var b strings.Builder
	b.WriteString(runnerSyntaxHeader("factory"))
	b.WriteString(" {\n")
	b.WriteString("  ")
	b.WriteString(runnerSyntaxHeader("imports"))
	b.WriteString(" { core: 'example.com/core' }\n\n")
	b.WriteString("  ")
	b.WriteString(runnerSyntaxHeader("actions"))
	b.WriteString(" { say: core.echo { echo: 'x' } }\n")
	b.WriteString("}\n")
	return b.String()
}

func runnerSyntaxHeader(name string) string {
	return name + ":"
}
