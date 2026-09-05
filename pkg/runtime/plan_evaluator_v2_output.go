package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (e *Executor) planEvaluationV2OutputRequest(
	evaluation *planEvaluationV2,
	node *Node,
) (planStepV2Request, error) {
	if e == nil {
		return planStepV2Request{}, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return planStepV2Request{}, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil {
		return planStepV2Request{}, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return planStepV2Request{}, fmt.Errorf("output node is required")
	}
	if node.Kind != NodeOutput || node.Composite != "" || node.IsComposite() {
		return planStepV2Request{}, fmt.Errorf("%s: node must be a root output", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeOutput); err != nil {
		return planStepV2Request{}, err
	}
	if node.ForEach != nil {
		return planStepV2Request{}, fmt.Errorf("%s: output cannot declare for-each", node.Address)
	}
	if node.Body == nil {
		return planStepV2Request{}, fmt.Errorf("%s: output expression is required", node.Address)
	}
	address, expression := node.Address, node.Body
	dependencies := planEvaluationV2Dependencies(e.DAG.Edges[address])
	planningDependencies := slices.Clone(dependencies)
	analyzer := e.sensitivityAnalyzer()
	sensitive := analyzer.exprSensitive(expression, analyzer.scopeFor(""))
	if e.SyntaxSource != nil {
		for _, output := range e.SyntaxSource.Outputs {
			if output.Name.Name == strings.TrimPrefix(address, "output.") {
				sensitive = sensitive || sensitiveDecl(output.Body)
				break
			}
		}
	}
	return planStepV2Request{
		Address: address, Kind: NodeOutput, DependsOn: dependencies,
		Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
			if ctx == nil {
				return nil, fmt.Errorf("output planning context is required")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var locals map[string]lang.Expr
			if evaluation.run.eval.locals != nil {
				locals = evaluation.run.eval.locals.exprs
			}
			evaluator := partialEvaluator{
				ec: evaluation.run.eval, locals: locals, expanding: map[string]bool{},
			}
			value, _, err := evaluator.element(expression)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", address, err)
			}
			encoded, err := encodePlanningValue(typecheck.TUnknown(), value)
			if err != nil {
				return nil, fmt.Errorf("%s: output value: %w", address, err)
			}
			return planOutputStep(outputPlanningRequest{
				Address: address, DependsOn: planningDependencies, Value: encoded, Sensitive: sensitive,
			})
		},
	}, nil
}
