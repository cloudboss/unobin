package codegen

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

type syntaxEmission struct {
	Literal  string
	UsesLang bool
}

func emitSyntaxFactoryBody(
	body syntax.FactoryBody,
	spanName SyntaxSpanNamer,
) (syntaxEmission, error) {
	var emitter syntaxEmitter
	if err := encodeSyntaxFactoryBody(&emitter, body, spanName); err != nil {
		return syntaxEmission{}, err
	}
	return syntaxEmission{Literal: emitter.String(), UsesLang: emitter.usesLang}, nil
}

type syntaxEmitter struct {
	strings.Builder
	usesLang bool
}

func (e *syntaxEmitter) node(n lang.Node, spanName SyntaxSpanNamer) (string, error) {
	if n != nil {
		e.usesLang = true
	}
	return encodeNodeString(n, spanName)
}
