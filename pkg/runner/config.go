package runner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type parsedConfig struct {
	stack *syntax.StackFile
}

// parseConfigFile reads, parses, and validates the stack file at path.
// An empty path returns nil with no error so callers can pass it through
// the no-config branch of each section reader uniformly. Each cobra command
// parses the config once at the top and threads the result into the section
// readers, so the file is read once per invocation regardless of how many
// sections the command needs.
func parseConfigFile(path string) (*parsedConfig, error) {
	if path == "" {
		return nil, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseConfigSource(path, src)
}

func parseConfigSource(path string, src []byte) (*parsedConfig, error) {
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
	return &parsedConfig{stack: sf.Stack}, nil
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

func configStack(config *parsedConfig) *syntax.StackFile {
	if config == nil {
		return nil
	}
	return config.stack
}

func configEvalContext(config *parsedConfig) *runtime.EvalContext {
	return runtime.NewEvalContextFromLocals(stackLocalExprs(configStack(config)))
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
