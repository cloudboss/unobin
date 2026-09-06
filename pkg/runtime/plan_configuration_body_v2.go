package runtime

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func planConfigurationBodyV2(
	body lang.Expr, scope *EvalContext, fields []typecheck.ObjectField,
) (map[string]any, error) {
	if _, ok := body.(*lang.ObjectLit); ok {
		values, _, err := planEvalBody(body, scope)
		return values, err
	}
	value, err := Eval(body, scope)
	if err != nil {
		if !errors.Is(err, ErrEvalNotFound) {
			return nil, err
		}
		var locals map[string]lang.Expr
		if scope.locals != nil {
			locals = scope.locals.exprs
		}
		value, _, err = partialValue(body, scope, locals)
		if err != nil {
			return nil, err
		}
	}
	if pending, ok := value.(PendingValue); ok {
		values := make(map[string]any, len(fields))
		for _, field := range fields {
			values[field.Name] = pending
		}
		return values, nil
	}
	values, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("configuration body must evaluate to an object, got %s",
			lang.TypeMessage(value))
	}
	return values, nil
}

func (e *Executor) configurationSensitivePathsV2(node *Node) []string {
	analyzer := e.sensitivityAnalyzer()
	if _, ok := node.Body.(*lang.ObjectLit); !ok &&
		analyzer.exprSensitive(node.Body, analyzer.scopeFor(node.Composite)) {
		return []string{""}
	}
	return planEvaluationV2SensitivePaths(analyzer.sensitiveInputs(node.Body, node.Composite))
}
