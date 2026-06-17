package runner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type parsedStack struct {
	stack *syntax.StackFile
}

// parseStackFile reads, parses, and validates the stack file at path.
// An empty path returns nil with no error so callers can pass it through
// the no-stack-file branch of each section reader uniformly. Each cobra command
// parses the stack file once at the top and threads it into the section
// readers, so the file is read once per invocation regardless of how many
// sections the command needs.
func parseStackFile(path string) (*parsedStack, error) {
	if path == "" {
		return nil, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseStackSource(path, src)
}

func parseStackSource(path string, src []byte) (*parsedStack, error) {
	raw, err := lang.ParseSource(path, src)
	if err != nil {
		return nil, err
	}
	if !hasStackDeclaration(raw) {
		return nil, fmt.Errorf("%s must declare stack", filepath.Base(path))
	}
	sf, err := syntax.ParseSource(path, src)
	if err != nil {
		return nil, err
	}
	if sf.Kind != syntax.FileStack || sf.Stack == nil {
		return nil, fmt.Errorf("%s: expected stack declaration", path)
	}
	if verrs := syntax.ValidateFile(sf); verrs.Len() > 0 {
		return nil, verrs.Err()
	}
	return &parsedStack{stack: sf.Stack}, nil
}

func hasStackDeclaration(f *lang.File) bool {
	if f == nil || f.Body == nil {
		return false
	}
	for _, fld := range f.Body.Fields {
		if fld.Decl != nil {
			continue
		}
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "stack" {
			return true
		}
	}
	return false
}

func stackFile(config *parsedStack) *syntax.StackFile {
	if config == nil {
		return nil
	}
	return config.stack
}

func stackEvalContext(config *parsedStack) *runtime.EvalContext {
	return runtime.NewEvalContextFromLocals(stackLocalExprs(stackFile(config)))
}

func stackLocalExprs(stack *syntax.StackFile) map[string]lang.Expr {
	if stack == nil || len(stack.Locals) == 0 {
		return nil
	}
	exprs := make(map[string]lang.Expr, len(stack.Locals))
	for _, local := range stack.Locals {
		exprs[local.Name.Name] = local.Value
	}
	return exprs
}
