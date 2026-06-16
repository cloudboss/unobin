package resolve

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// ValidateSyntaxCompositeBody checks a typed composite body against the
// requirements for its declared kind.
func ValidateSyntaxCompositeBody(kind, typeName string, body syntax.FactoryBody) []error {
	return validateCompositeCounts(
		kind,
		typeName,
		len(body.Resources),
		len(body.Actions),
		len(body.Outputs),
	)
}

func validateCompositeCounts(kind, typeName string, resources, actions, outputs int) []error {
	var errs []error
	add := func(msg string) {
		errs = append(errs, fmt.Errorf("composite %q (%s): %s", typeName, kind, msg))
	}
	switch kind {
	case "data":
		if outputs == 0 {
			add("a data composite must declare at least one output")
		}
		if resources > 0 {
			add("a data composite must not contain resources")
		}
		if actions > 0 {
			add("a data composite must not contain actions")
		}
	case "action":
		if actions == 0 {
			add("an action composite must contain at least one action")
		}
		if resources > 0 {
			add("an action composite must not contain resources")
		}
	case "resource":
		if resources == 0 {
			add("a resource composite must contain at least one resource")
		}
	}
	return errs
}
