package runner

import (
	"fmt"
	"os"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// parseConfigFile reads, parses, and validates the config.ub at path.
// An empty path returns a nil file with no error so callers can pass
// it through the no-config branch of each section reader uniformly.
// Each cobra command parses the config once at the top and threads
// the result into verifyStackEnvelope and the doXxx implementation,
// so the file is read once per invocation regardless of how many
// sections the command needs.
func parseConfigFile(path string) (*lang.File, error) {
	if path == "" {
		return nil, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := lang.ParseSource(path, src)
	if err != nil {
		return nil, err
	}
	if hasStackDeclaration(f) {
		return lowerStackConfig(f)
	}
	f.Kind = lang.FileConfig
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return f, nil
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

func lowerStackConfig(f *lang.File) (*lang.File, error) {
	sf, serrs := syntax.LowerFile(f)
	if serrs.Len() > 0 {
		return nil, serrs.Err()
	}
	if sf.Kind != syntax.FileStack || sf.Stack == nil {
		return nil, fmt.Errorf("%s: expected stack declaration", f.Path)
	}
	if verrs := syntax.ValidateFile(sf); verrs.Len() > 0 {
		return nil, verrs.Err()
	}
	return &lang.File{
		S:        f.S,
		Kind:     lang.FileConfig,
		Path:     f.Path,
		Body:     syntax.StackFileObject(sf.Stack),
		Comments: f.Comments,
	}, nil
}
