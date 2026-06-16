package resolve

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// ValidateCompositeBody checks a generic composite body against the
// requirements for its declared kind.
func ValidateCompositeBody(kind, typeName string, f *lang.File) []error {
	return validateCompositeCounts(
		kind,
		typeName,
		kindLeafCount(f, "resources"),
		kindLeafCount(f, "actions"),
		outputCount(f),
	)
}

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

// kindLeafCount counts the leaf entries in a resources, data, or actions block.
func kindLeafCount(f *lang.File, block string) int {
	obj := lang.TopLevelBlock(f, block)
	if obj == nil {
		return 0
	}
	count := 0
	for _, fld := range obj.Fields {
		switch {
		case fld.Decl != nil && !fld.Decl.Default:
			count++
		case fld.Key.Kind == lang.FieldPath && len(fld.Key.Path) == 3:
			count++
		}
	}
	return count
}

// outputCount counts the named fields in the outputs block.
func outputCount(f *lang.File) int {
	obj := lang.TopLevelBlock(f, "outputs")
	if obj == nil {
		return 0
	}
	count := 0
	for _, fld := range obj.Fields {
		if fld.Key.Kind == lang.FieldIdent && !fld.Key.IsMeta() {
			count++
		}
	}
	return count
}
