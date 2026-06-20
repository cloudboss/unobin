package goschema

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (w *walker) lookupConfigurationFields(
	ref typeRef,
	init *ast.CompositeLit,
) ([]typecheck.ObjectField, []lang.DefaultSpec, map[string]bool) {
	fields, sensitive := w.lookupObjectFields(ref)
	if fields == nil {
		return nil, nil, nil
	}
	defaults := w.lookupConfigurationDefaults(ref, init)
	markDefaultedFields(fields, defaults)
	return fields, defaults, sensitive
}

func (w *walker) lookupConfigurationDefaults(
	ref typeRef,
	init *ast.CompositeLit,
) []lang.DefaultSpec {
	if init == nil {
		return nil
	}
	if ref.PkgAlias == "" {
		return w.configDefaultsFromPackage(ref.TypeName, init, nil)
	}
	importPath, ok := w.imports[ref.PkgAlias]
	if !ok {
		return nil
	}
	sub := w.sub(importPath)
	if sub == nil {
		return nil
	}
	return sub.configDefaultsFromPackage(ref.TypeName, init, nil)
}

func (w *walker) configDefaultsFromPackage(
	typeName string,
	init *ast.CompositeLit,
	path []string,
) []lang.DefaultSpec {
	if init == nil {
		return nil
	}
	spec := findTypeSpec(w.files, typeName)
	if spec == nil {
		return nil
	}
	if spec.Assign != token.NoPos {
		switch t := spec.Type.(type) {
		case *ast.Ident:
			return w.configDefaultsFromPackage(t.Name, init, path)
		case *ast.SelectorExpr:
			pkg, ok := identName(t.X)
			if !ok {
				return nil
			}
			importPath, ok := w.imports[pkg]
			if !ok {
				return nil
			}
			sub := w.sub(importPath)
			if sub == nil {
				return nil
			}
			return sub.configDefaultsFromPackage(t.Sel.Name, init, path)
		default:
			return nil
		}
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	return w.configDefaultsFromStruct(st, init, path)
}

func (w *walker) configDefaultsFromStruct(
	st *ast.StructType,
	init *ast.CompositeLit,
	path []string,
) []lang.DefaultSpec {
	if st == nil || st.Fields == nil || init == nil {
		return nil
	}
	inits := keyedCompositeFields(init)
	var out []lang.DefaultSpec
	for _, fld := range st.Fields.List {
		name, skip, _, _ := parseUBFieldTag(fld.Tag)
		if skip {
			continue
		}
		for _, fieldName := range fld.Names {
			kebab := name
			if kebab == "" {
				kebab = lang.PascalToKebab(fieldName.Name)
			}
			fieldInit := inits[fieldName.Name]
			out = append(out, w.configDefaultsForField(kebab, fld.Type, fieldInit, path)...)
		}
	}
	return out
}

func (w *walker) configDefaultsForField(
	name string,
	fieldType ast.Expr,
	fieldInit ast.Expr,
	path []string,
) []lang.DefaultSpec {
	optional := false
	fieldType = unwrapStar(fieldType, &optional)
	fieldPath := append(append([]string{}, path...), name)
	if wrapper, ok := w.cfgWrapperRef(fieldType); ok {
		return w.configDefaultsForWrapper(wrapper, fieldInit, fieldPath, optional)
	}
	init := unwrapModuleLiteral(fieldInit)
	if init == nil {
		return nil
	}
	ref, ok := outputTypeRef(fieldType)
	if !ok {
		return nil
	}
	if ref.PkgAlias == "" {
		return w.configDefaultsFromPackage(ref.TypeName, init, fieldPath)
	}
	importPath, ok := w.imports[ref.PkgAlias]
	if !ok {
		return nil
	}
	sub := w.sub(importPath)
	if sub == nil {
		return nil
	}
	return sub.configDefaultsFromPackage(ref.TypeName, init, fieldPath)
}

func unwrapStar(e ast.Expr, optional *bool) ast.Expr {
	if star, ok := e.(*ast.StarExpr); ok {
		*optional = true
		return star.X
	}
	return e
}

type cfgWrapperRef struct {
	name string
	elem ast.Expr
}

func (w *walker) cfgWrapperRef(e ast.Expr) (cfgWrapperRef, bool) {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		pkg, ok := identName(v.X)
		if !ok || w.imports[pkg] != cfgPkgPath {
			return cfgWrapperRef{}, false
		}
		return cfgWrapperRef{name: v.Sel.Name}, true
	case *ast.IndexExpr:
		wrapper, ok := w.cfgWrapperRef(v.X)
		if !ok {
			return cfgWrapperRef{}, false
		}
		wrapper.elem = v.Index
		return wrapper, true
	case *ast.IndexListExpr:
		wrapper, ok := w.cfgWrapperRef(v.X)
		if !ok || len(v.Indices) != 1 {
			return cfgWrapperRef{}, false
		}
		wrapper.elem = v.Indices[0]
		return wrapper, true
	}
	return cfgWrapperRef{}, false
}

func (w *walker) configDefaultsForWrapper(
	wrapper cfgWrapperRef,
	fieldInit ast.Expr,
	path []string,
	optional bool,
) []lang.DefaultSpec {
	if wrapper.name == "Object" {
		return w.configDefaultsForObjectWrapper(wrapper, fieldInit, path)
	}
	if !optional || fieldInit == nil {
		return nil
	}
	fieldPath := strings.Join(path, ".")
	init := unwrapModuleLiteral(fieldInit)
	if init == nil {
		w.addWarnf("field %s: config default is not a readable literal", fieldPath)
		return nil
	}
	value, ok := w.wrapperDefaultValue(wrapper, init, fieldPath)
	if !ok {
		return nil
	}
	return []lang.DefaultSpec{{Field: "var." + fieldPath, Value: lang.Render(value)}}
}

func (w *walker) configDefaultsForObjectWrapper(
	wrapper cfgWrapperRef,
	fieldInit ast.Expr,
	path []string,
) []lang.DefaultSpec {
	init := unwrapModuleLiteral(fieldInit)
	if init == nil {
		return nil
	}
	valueInit := unwrapModuleLiteral(compositeField(init, "Value"))
	if valueInit == nil {
		return nil
	}
	ref, ok := outputTypeRef(wrapper.elem)
	if !ok {
		return nil
	}
	if ref.PkgAlias == "" {
		return w.configDefaultsFromPackage(ref.TypeName, valueInit, path)
	}
	importPath, ok := w.imports[ref.PkgAlias]
	if !ok {
		return nil
	}
	sub := w.sub(importPath)
	if sub == nil {
		return nil
	}
	return sub.configDefaultsFromPackage(ref.TypeName, valueInit, path)
}

func (w *walker) wrapperDefaultValue(
	wrapper cfgWrapperRef,
	init *ast.CompositeLit,
	path string,
) (any, bool) {
	defaultExpr, hasDefault := compositeFieldOK(init, "Default")
	switch wrapper.name {
	case "String", "Integer", "Number", "Boolean":
		value, ok := scalarDefaultValue(wrapper.name, defaultExpr, hasDefault)
		if !ok {
			w.addWarnf("field %s: config default is not a supported literal", path)
			return nil, false
		}
		return value, true
	case "List":
		if !hasDefault {
			return nil, false
		}
		value, ok := w.listDefaultValue(wrapper.elem, defaultExpr, path)
		return value, ok
	case "Map":
		if !hasDefault {
			return nil, false
		}
		value, ok := w.mapDefaultValue(wrapper.elem, defaultExpr, path)
		return value, ok
	}
	return nil, false
}

func scalarDefaultValue(kind string, expr ast.Expr, present bool) (any, bool) {
	if !present {
		switch kind {
		case "String":
			return "", true
		case "Integer":
			return int64(0), true
		case "Number":
			return float64(0), true
		case "Boolean":
			return false, true
		}
	}
	switch kind {
	case "String":
		return sourceString(expr)
	case "Integer":
		return sourceInt(expr)
	case "Number":
		return sourceNumber(expr)
	case "Boolean":
		return boolLit(expr)
	}
	return nil, false
}

func (w *walker) listDefaultValue(elem ast.Expr, expr ast.Expr, path string) (any, bool) {
	lit := unwrapModuleLiteral(expr)
	if lit == nil || len(lit.Elts) == 0 {
		return nil, false
	}
	elemWrapper, ok := w.cfgWrapperRef(elem)
	if !ok {
		w.addWarnf("field %s: list default element type is not supported", path)
		return nil, false
	}
	out := make([]any, 0, len(lit.Elts))
	for _, el := range lit.Elts {
		value, ok := wrapperItemValue(elemWrapper.name, el)
		if !ok {
			w.addWarnf("field %s: list default item is not a supported literal", path)
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

func (w *walker) mapDefaultValue(elem ast.Expr, expr ast.Expr, path string) (any, bool) {
	lit := unwrapModuleLiteral(expr)
	if lit == nil || len(lit.Elts) == 0 {
		return nil, false
	}
	elemWrapper, ok := w.cfgWrapperRef(elem)
	if !ok {
		w.addWarnf("field %s: map default element type is not supported", path)
		return nil, false
	}
	out := make(map[string]any, len(lit.Elts))
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			w.addWarnf("field %s: map default entry is not keyed", path)
			return nil, false
		}
		key, ok := sourceString(kv.Key)
		if !ok {
			w.addWarnf("field %s: map default key is not a string literal", path)
			return nil, false
		}
		value, ok := wrapperItemValue(elemWrapper.name, kv.Value)
		if !ok {
			w.addWarnf("field %s: map default item is not a supported literal", path)
			return nil, false
		}
		out[key] = value
	}
	return out, true
}

func wrapperItemValue(kind string, expr ast.Expr) (any, bool) {
	init := unwrapModuleLiteral(expr)
	if init == nil {
		return nil, false
	}
	valueExpr, present := compositeFieldOK(init, "Value")
	return scalarDefaultValue(kind, valueExpr, present)
}

func keyedCompositeFields(lit *ast.CompositeLit) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	if lit == nil {
		return out
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := identName(kv.Key)
		if !ok {
			continue
		}
		out[name] = kv.Value
	}
	return out
}

func compositeField(lit *ast.CompositeLit, name string) ast.Expr {
	expr, _ := compositeFieldOK(lit, name)
	return expr
}

func compositeFieldOK(lit *ast.CompositeLit, name string) (ast.Expr, bool) {
	if lit == nil {
		return nil, false
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		got, ok := identName(kv.Key)
		if ok && got == name {
			return kv.Value, true
		}
	}
	return nil, false
}

func sourceString(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func sourceInt(expr ast.Expr) (int64, bool) {
	sign := int64(1)
	if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		sign = -1
		expr = u.X
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	n, err := strconv.ParseInt(lit.Value, 0, 64)
	if err != nil {
		return 0, false
	}
	return sign * n, true
}

func sourceNumber(expr ast.Expr) (float64, bool) {
	sign := 1.0
	if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		sign = -1
		expr = u.X
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok || (lit.Kind != token.INT && lit.Kind != token.FLOAT) {
		return 0, false
	}
	n, err := strconv.ParseFloat(lit.Value, 64)
	if err != nil {
		return 0, false
	}
	return sign * n, true
}

func markDefaultedFields(fields []typecheck.ObjectField, defaults []lang.DefaultSpec) {
	for _, def := range defaults {
		if def.Optional {
			continue
		}
		path, ok := strings.CutPrefix(def.Field, "var.")
		if !ok || path == "" {
			continue
		}
		markDefaultedPath(fields, strings.Split(path, "."))
	}
}

func markDefaultedPath(fields []typecheck.ObjectField, path []string) bool {
	if len(path) == 0 {
		return false
	}
	for i := range fields {
		if fields[i].Name != path[0] {
			continue
		}
		if len(path) == 1 {
			fields[i].Defaulted = true
			return true
		}
		t := fields[i].Type
		if t.Kind != typecheck.Object {
			return false
		}
		if markDefaultedPath(t.Fields, path[1:]) {
			fields[i].Type = t
			return true
		}
	}
	return false
}
