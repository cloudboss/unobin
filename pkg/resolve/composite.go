package resolve

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ValidateCompositeBody checks a composite body against the floor and
// ceiling rules for its kind, which comes from the file's `<kind>-`
// name prefix:
//
//   - data: at least one output, may hold data, no resources, no actions.
//   - action: at least one action, may hold data, no resources; outputs
//     are optional.
//   - resource: at least one resource, may hold data and actions; outputs
//     are optional.
//
// typeName names the composite in the messages. Returns one error per
// violated rule, in a fixed order, so a body reports every problem at once.
// The resolver does not run this during the walk; the compile command runs
// it over each resolved library so that print-graph and fetch stay lenient.
func ValidateCompositeBody(kind, typeName string, f *lang.File) []error {
	var errs []error
	add := func(msg string) {
		errs = append(errs, fmt.Errorf("composite %q (%s): %s", typeName, kind, msg))
	}
	resources := kindLeafCount(f, "resources")
	actions := kindLeafCount(f, "actions")
	switch kind {
	case "data":
		if outputCount(f) == 0 {
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

// kindLeafCount counts the leaf entries in a resources, data, or
// actions block: one per `alias.type.name` dotted key. A key that is not
// a three-segment path contributes nothing, so an empty or absent block
// is zero.
func kindLeafCount(f *lang.File, block string) int {
	obj := topLevelBlock(f, block)
	if obj == nil {
		return 0
	}
	count := 0
	for _, fld := range obj.Fields {
		if fld.Key.Kind == lang.FieldPath && len(fld.Key.Path) == 3 {
			count++
		}
	}
	return count
}

// outputCount counts the named fields in the outputs block.
func outputCount(f *lang.File) int {
	obj := topLevelBlock(f, "outputs")
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

// topLevelBlock returns the named top-level block as an object, or nil
// when it is absent or not an object.
func topLevelBlock(f *lang.File, name string) *lang.ObjectLit {
	if f == nil || f.Body == nil {
		return nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == lang.FieldIdent && !fld.Key.IsMeta() && fld.Key.Name == name {
			if obj, ok := fld.Value.(*lang.ObjectLit); ok {
				return obj
			}
		}
	}
	return nil
}
