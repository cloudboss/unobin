package runner

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
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
	if p == nil {
		return nil
	}
	if p.syntaxBody != nil {
		return syntaxInputBlock(p.syntaxBody.Inputs)
	}
	return lang.TopLevelBlock(p.file, "inputs")
}

func (p *parsedFactory) constraints() *lang.ArrayLit {
	if p == nil {
		return nil
	}
	if p.syntaxBody != nil {
		return syntaxConstraints(p.syntaxBody.Constraints)
	}
	return lang.TopLevelArray(p.file, "constraints")
}

func (p *parsedFactory) internalConfigurations() map[string]map[string]bool {
	if p == nil {
		return map[string]map[string]bool{}
	}
	if p.syntaxBody != nil {
		return runtime.InternalSyntaxConfigurationNames(*p.syntaxBody)
	}
	return runtime.InternalConfigurationNames(p.file)
}

func (p *parsedFactory) inputs() []factoryInputDecl {
	if p == nil {
		return nil
	}
	if p.syntaxBody != nil {
		return syntaxInputs(p.syntaxBody.Inputs)
	}
	return genericInputs(lang.TopLevelBlock(p.file, "inputs"))
}

func (p *parsedFactory) outputs() []factoryOutputDecl {
	if p == nil {
		return nil
	}
	if p.syntaxBody != nil {
		return syntaxOutputs(p.syntaxBody.Outputs)
	}
	return genericOutputs(lang.TopLevelBlock(p.file, "outputs"))
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

func genericInputs(block *lang.ObjectLit) []factoryInputDecl {
	if block == nil {
		return nil
	}
	out := make([]factoryInputDecl, 0, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		decl, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		input := factoryInputDecl{name: fld.Key.Name}
		input.typeExpr, input.description, input.defaultExpr = inputFields(decl)
		out = append(out, input)
	}
	return out
}

func inputFields(decl *lang.ObjectLit) (lang.Expr, string, lang.Expr) {
	var typeExpr lang.Expr
	description, defaultExpr := inputMetadata(decl)
	if decl == nil {
		return nil, description, defaultExpr
	}
	for _, fld := range decl.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "type" {
			typeExpr = fld.Value
			break
		}
	}
	return typeExpr, description, defaultExpr
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

func genericOutputs(block *lang.ObjectLit) []factoryOutputDecl {
	if block == nil {
		return nil
	}
	out := make([]factoryOutputDecl, 0, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		body, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		out = append(out, factoryOutputDecl{name: fld.Key.Name, body: body})
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
