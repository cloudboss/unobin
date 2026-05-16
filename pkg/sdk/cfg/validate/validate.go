// Package validate provides standard Validator constructors for cfg
// configuration fields. Each constructor returns a cfg.Validator
// whose Describe reports its kind and parameters so the schema
// commands and editor tooling can render the constraint
// declaratively rather than as an opaque function.
package validate

import (
	"fmt"
	"regexp"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// Pattern returns a validator that requires a string value to match
// a regular expression. The pattern compiles at construction; a
// malformed pattern panics.
func Pattern(re string) cfg.Validator {
	return &patternValidator{re: regexp.MustCompile(re), pattern: re}
}

type patternValidator struct {
	re      *regexp.Regexp
	pattern string
}

func (p *patternValidator) Check(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("pattern requires a string, got %T", v)
	}
	if !p.re.MatchString(s) {
		return fmt.Errorf("value %q does not match pattern %s", s, p.pattern)
	}
	return nil
}

func (p *patternValidator) Describe() cfg.ValidatorDesc {
	return cfg.ValidatorDesc{
		Kind:   "pattern",
		Params: map[string]any{"pattern": p.pattern},
	}
}

// Range returns a validator that requires an integer value to fall
// within [min, max] inclusive.
func Range(min, max int64) cfg.Validator {
	return &rangeValidator{min: min, max: max}
}

type rangeValidator struct {
	min, max int64
}

func (r *rangeValidator) Check(v any) error {
	n, ok := v.(int64)
	if !ok {
		return fmt.Errorf("range requires an integer, got %T", v)
	}
	if n < r.min || n > r.max {
		return fmt.Errorf("value %d outside range [%d, %d]", n, r.min, r.max)
	}
	return nil
}

func (r *rangeValidator) Describe() cfg.ValidatorDesc {
	return cfg.ValidatorDesc{
		Kind:   "range",
		Params: map[string]any{"min": r.min, "max": r.max},
	}
}

// All returns a validator that runs each child in order. The first
// failure stops the chain.
func All(vs ...cfg.Validator) cfg.Validator {
	return &allValidator{vs: vs}
}

type allValidator struct {
	vs []cfg.Validator
}

func (a *allValidator) Check(v any) error {
	for _, x := range a.vs {
		if err := x.Check(v); err != nil {
			return err
		}
	}
	return nil
}

func (a *allValidator) Describe() cfg.ValidatorDesc {
	children := make([]cfg.ValidatorDesc, len(a.vs))
	for i, x := range a.vs {
		children[i] = x.Describe()
	}
	return cfg.ValidatorDesc{
		Kind:   "all",
		Params: map[string]any{"children": children},
	}
}

// Func returns a validator backed by an arbitrary Go function. The
// description tells operators and editor tooling what the function
// checks, since the function body itself is opaque to introspection.
func Func(description string, fn func(any) error) cfg.Validator {
	return &funcValidator{description: description, fn: fn}
}

type funcValidator struct {
	description string
	fn          func(any) error
}

func (f *funcValidator) Check(v any) error {
	return f.fn(v)
}

func (f *funcValidator) Describe() cfg.ValidatorDesc {
	return cfg.ValidatorDesc{
		Kind:   "custom",
		Params: map[string]any{"description": f.description},
	}
}
