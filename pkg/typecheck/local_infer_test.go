package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
)

func TestInferLocalReference(t *testing.T) {
	scope := &Scope{
		LookupLocal: func(name string) (Type, bool) {
			if name == "count" {
				return TInteger(), true
			}
			return Type{}, false
		},
	}
	got := Infer(parseExpr(t, "local.count"), TUnknown(), scope, lang.NewErrorList(0))
	assert.True(t, got.Equal(TInteger()), "got %s", got)
}

func TestInferLocalNavigatesObject(t *testing.T) {
	scope := &Scope{
		LookupLocal: func(name string) (Type, bool) {
			if name == "lb" {
				return TObject([]ObjectField{{Name: "host", Type: TString()}}), true
			}
			return Type{}, false
		},
	}
	got := Infer(parseExpr(t, "local.lb.host"), TUnknown(), scope, lang.NewErrorList(0))
	assert.True(t, got.Equal(TString()), "got %s", got)
}

func TestInferLocalUnknownName(t *testing.T) {
	scope := &Scope{
		LookupLocal: func(string) (Type, bool) { return Type{}, false },
	}
	got := Infer(parseExpr(t, "local.nope"), TUnknown(), scope, lang.NewErrorList(0))
	assert.True(t, got.Equal(TUnknown()), "got %s", got)
}

func TestInferLocalNilResolver(t *testing.T) {
	got := Infer(parseExpr(t, "local.x"), TUnknown(), &Scope{}, lang.NewErrorList(0))
	assert.True(t, got.Equal(TUnknown()), "got %s", got)
}
