package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// TriggerAlways is the literal an action uses to opt into running every
// time, regardless of stored state.
const TriggerAlways = "always"

// TriggerDecision is what the runtime computes for an action node before
// dispatching it: a hash to compare against state and a flag for the
// always-rerun escape hatch.
type TriggerDecision struct {
	Hash        string
	AlwaysRerun bool
	HasExplicit bool
}

// ComputeTrigger evaluates the action node's `@trigger` meta key (if any)
// and returns the resulting decision. Without `@trigger`, the hash is
// taken over the action's selector and evaluated inputs so any visible
// change to the body causes a rerun. With `@trigger: 'always'`,
// AlwaysRerun is true and the hash is empty.
func ComputeTrigger(n *Node, inputs map[string]any, ec *EvalContext) (TriggerDecision, error) {
	obj, ok := n.Body.(*lang.ObjectLit)
	if !ok {
		return TriggerDecision{}, fmt.Errorf("body must be an object literal")
	}
	var triggerExpr lang.Expr
	for _, fld := range obj.Fields {
		if fld.Key.IsMeta() && fld.Key.Name == "@trigger" {
			triggerExpr = fld.Value
			break
		}
	}
	if triggerExpr != nil {
		val, err := Eval(triggerExpr, ec)
		if err != nil {
			// Trigger references something the EvalContext can't yet
			// resolve (e.g., a fresh resource's outputs at plan time).
			// Return an empty hash so the action reruns; apply
			// recomputes the hash against fresh state and stores it.
			return TriggerDecision{HasExplicit: true}, nil
		}
		if s, ok := val.(string); ok && s == TriggerAlways {
			return TriggerDecision{AlwaysRerun: true, HasExplicit: true}, nil
		}
		hash, err := hashJSON(triggerHashValue(n, val))
		if err != nil {
			return TriggerDecision{}, err
		}
		return TriggerDecision{Hash: hash, HasExplicit: true}, nil
	}
	hash, err := hashJSON(triggerHashValue(n, inputs))
	if err != nil {
		return TriggerDecision{}, err
	}
	return TriggerDecision{Hash: hash}, nil
}

func triggerHashValue(n *Node, value any) any {
	return map[string]any{
		"selector": selectorForNode(n),
		"value":    value,
	}
}

func hashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("trigger hash: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
