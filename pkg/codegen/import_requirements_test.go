package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestGenerateImportsFollowExpressions(t *testing.T) {
	body := syntax.FactoryBody{StateMoves: []syntax.StateMoveDecl{{
		From: &syntax.StateMoveRef{Ref: runtime.EntryRef{Address: "resource.lang.old"}},
		To:   &syntax.StateMoveRef{Ref: runtime.EntryRef{Address: "resource.lang.new"}},
	}}}
	for _, test := range []struct {
		name string
		main bool
		body syntax.FactoryBody
	}{
		{name: "factory address", main: true, body: body},
		{name: "empty composite"},
		{name: "composite address", body: body},
	} {
		t.Run(test.name, func(t *testing.T) {
			var source []byte
			var err error
			if test.main {
				source, err = Generate(Input{FactoryName: "example", FactoryBody: test.body})
			} else {
				source, err = GenerateLibrary("example", program.Library{
					Name: "example", Composites: []program.Composite{{
						Category: "resource", Export: "example", Body: test.body,
					}},
				}, nil)
			}
			require.NoError(t, err)
			require.NotContains(t, string(source), `"github.com/cloudboss/unobin/pkg/lang"`)
			if testing.Short() {
				return
			}
			dir := t.TempDir()
			module := fmt.Sprintf("module example.test/check\n\ngo 1.26.2\n"+
				"require github.com/cloudboss/unobin v0.0.0\n"+
				"replace github.com/cloudboss/unobin => %s\n", findUnobinRoot(t))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "generated.go"), source, 0o644))
			build := exec.Command("go", "build", "-mod=mod", "-buildvcs=false", "-o",
				filepath.Join(dir, "compiled"), ".")
			build.Dir = dir
			output, err := build.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}
