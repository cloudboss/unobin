package runner

import (
	"os"

	"github.com/cloudboss/unobin/pkg/lang"
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
	f.Kind = lang.FileConfig
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return f, nil
}
