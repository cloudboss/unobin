package runner

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

type factoryInputDecl struct {
	name        string
	typeExpr    lang.Expr
	description string
	defaultExpr lang.Expr
}

type factoryOutputDecl struct {
	name string
	body *lang.ObjectLit
}

func (p *parsedFactory) inputBlock() *lang.ObjectLit {
	if p == nil || p.syntaxBody == nil {
		return nil
	}
	return syntaxInputBlock(p.syntaxBody.Inputs)
}

func (p *parsedFactory) constraints() *lang.ArrayLit {
	if p == nil || p.syntaxBody == nil {
		return nil
	}
	return syntaxConstraints(p.syntaxBody.Constraints)
}

func (p *parsedFactory) inputs() []factoryInputDecl {
	if p == nil || p.syntaxBody == nil {
		return nil
	}
	return syntaxInputs(p.syntaxBody.Inputs)
}

func (p *parsedFactory) outputs() []factoryOutputDecl {
	if p == nil || p.syntaxBody == nil {
		return nil
	}
	return syntaxOutputs(p.syntaxBody.Outputs)
}

func (p *parsedFactory) sensitiveOutputs() map[string]bool {
	out := map[string]bool{}
	for _, output := range p.outputs() {
		if hasSensitiveOutput(output.body) {
			out[output.name] = true
		}
	}
	return out
}

func syntaxInputBlock(decls []syntax.InputDecl) *lang.ObjectLit {
	if len(decls) == 0 {
		return nil
	}
	obj := &lang.ObjectLit{S: decls[0].S}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, &lang.Field{
			S: decl.S,
			Key: lang.FieldKey{
				S:    decl.Name.S,
				Kind: lang.FieldIdent,
				Name: decl.Name.Name,
			},
			Value: decl.Body,
		})
	}
	return obj
}

func syntaxConstraints(decls []syntax.ConstraintDecl) *lang.ArrayLit {
	if len(decls) == 0 {
		return nil
	}
	arr := &lang.ArrayLit{S: decls[0].S}
	for _, decl := range decls {
		arr.Elements = append(arr.Elements, decl.Value)
	}
	return arr
}

func syntaxInputs(decls []syntax.InputDecl) []factoryInputDecl {
	out := make([]factoryInputDecl, 0, len(decls))
	for _, decl := range decls {
		input := factoryInputDecl{
			name:     decl.Name.Name,
			typeExpr: decl.Type,
		}
		input.description, input.defaultExpr = inputMetadata(decl.Body)
		out = append(out, input)
	}
	return out
}

func inputMetadata(decl *lang.ObjectLit) (string, lang.Expr) {
	if decl == nil {
		return "", nil
	}
	var description string
	var defaultExpr lang.Expr
	for _, fld := range decl.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		switch fld.Key.Name {
		case "description":
			if s, ok := fld.Value.(*lang.StringLit); ok {
				description = s.Value
			}
		case "default":
			defaultExpr = fld.Value
		}
	}
	return description, defaultExpr
}

func syntaxOutputs(decls []syntax.OutputDecl) []factoryOutputDecl {
	out := make([]factoryOutputDecl, 0, len(decls))
	for _, decl := range decls {
		out = append(out, factoryOutputDecl{name: decl.Name.Name, body: decl.Body})
	}
	return out
}

func hasSensitiveOutput(body *lang.ObjectLit) bool {
	if body == nil {
		return false
	}
	for _, fld := range body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@sensitive" {
			continue
		}
		b, ok := fld.Value.(*lang.BoolLit)
		return ok && b.Value
	}
	return false
}
