package runtime

import (
	"errors"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
)

func (e *Executor) checkPlanEvaluationV2Constraints(node *Node, inputs EncodedValue) error {
	fields, _ := inputs.ObjectFields()
	values := make(map[string]any, len(fields))
	deferred := make(map[string]bool)
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		value := fields[name]
		if value.HasPending() {
			deferred[name] = true
			continue
		}
		decoded, present, err := decodeConcreteValue(value, "constraint input")
		if err != nil {
			return err
		}
		if present {
			values[name] = decoded
		}
	}
	evaluate := func(expression lang.Expr, bindings []lang.EachBinding) (any, error) {
		context := &EvalContext{Inputs: values, Libraries: node.Libraries, MissingAsNull: true}
		ApplyBindings(context, bindings)
		return Eval(expression, context)
	}
	var violations []error
	if node.IsComposite() {
		violations = append(violations, lang.CheckPartialConstraints(
			compositeConstraints(node), values, evaluate, lang.DisplayNodeRelative, deferred,
		).Err())
	} else {
		library := e.librariesFor(node)[node.Alias]
		specs := library.Constraints[string(node.Kind)+"."+node.Type]
		entries, invalid := lang.ParseSpecs(specs)
		violations = append(violations, invalid.Err())
		for i, entry := range entries {
			if !entry.ReadsAny(deferred) {
				violations = append(violations,
					lang.CheckConstraintEntry(i, entry, values, evaluate, lang.DisplayNodeRelative).Err(),
				)
			}
		}
	}
	return errors.Join(violations...)
}
