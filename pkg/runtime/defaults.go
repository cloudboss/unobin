package runtime

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// applyInputDefaults fills the node type's declared defaults into the
// evaluated inputs. unresolved names the fields still waiting on an
// upstream output, which keep their pending placeholder rather than
// taking a default. A node without a library record or declared
// defaults changes nothing.
func (e *Executor) applyInputDefaults(
	n *Node, inputs map[string]any, unresolved map[string][]string,
) error {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok || lib == nil {
		return nil
	}
	specs := lib.Defaults[string(n.Kind)+"."+n.Type]
	if len(specs) == 0 {
		return nil
	}
	return overlayDefaults(inputs, specs, unresolved)
}

// overlayDefaults fills declared Value defaults into a body's evaluated
// inputs. A default applies when its field is left out and is not still
// waiting on an upstream output; a value the body set stays, null and
// the zero value included. A dotted field applies only when every
// parent object is present, so an absent optional object is not
// invented; an Optional marker fills nothing. The literal source in a
// spec evaluates with an empty context, and one that does not reduce
// reports an error naming the field.
func overlayDefaults(
	inputs map[string]any, specs []lang.DefaultSpec, unresolved map[string][]string,
) error {
	for _, s := range specs {
		if s.Optional {
			continue
		}
		path, ok := strings.CutPrefix(s.Field, "var.")
		if !ok {
			continue
		}
		segments := strings.Split(path, ".")
		if _, pending := unresolved[segments[0]]; pending {
			continue
		}
		target := inputs
		for _, parent := range segments[:len(segments)-1] {
			child, ok := target[parent].(map[string]any)
			if !ok {
				target = nil
				break
			}
			target = child
		}
		if target == nil {
			continue
		}
		leaf := segments[len(segments)-1]
		if _, ok := target[leaf]; ok {
			continue
		}
		val, err := evalDefaultLiteral(path, s.Value)
		if err != nil {
			return err
		}
		target[leaf] = val
	}
	return nil
}

// evalDefaultLiteral reduces a default's literal source to its value.
func evalDefaultLiteral(field, src string) (any, error) {
	expr, err := lang.ParseExpr("default", []byte(src))
	if err != nil {
		return nil, fmt.Errorf("default for %q: %v", field, err)
	}
	v, err := Eval(expr, &EvalContext{})
	if err != nil {
		return nil, fmt.Errorf("default for %q: %v", field, err)
	}
	return v, nil
}
