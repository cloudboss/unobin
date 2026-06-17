package runtime

import (
	"fmt"
	"time"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// NodeKind tags a Node with its source block.
type NodeKind string

const (
	NodeResource      NodeKind = "resource"
	NodeData          NodeKind = "data"
	NodeAction        NodeKind = "action"
	NodeOutput        NodeKind = "output"
	NodeConfiguration NodeKind = "configuration"
)

// Node is one addressable element of a stack: a single resource instance,
// data source, action, output, or composite call site. Address is the
// dotted form the language uses to reference the node from elsewhere,
// such as `resource.app`, `action.deploy`, or `output.cluster-arn`.
// Body is the source expression: an ObjectLit for resources, data,
// actions, and composites; any Expr for outputs.
//
// A node inside a composite stores the call site address in Composite so
// the runtime evaluates its body against the composite's scope rather
// than the root. Its address looks like `resource.app/resource.inner`,
// with the call site as a prefix joined by a single `/`. For a composite
// that itself calls another composite the chain continues:
// `resource.outer/resource.inner/resource.leaf`, and each node's
// Composite names its direct enclosing call site.
//
// CompositeSyntaxBody and Libraries are set only on a composite boundary
// (the call site node), and IsComposite reports that case. Libraries is
// the composite's resolved import table; the runtime resolves
// composite-internal node lookups against this map rather than the stack
// root's, so a composite can be reused without the caller importing every
// library it transitively uses.
type Node struct {
	Address             string
	Kind                NodeKind
	Alias               string
	Type                string
	Name                string
	Body                lang.Expr
	Composite           string
	CompositeSyntaxBody *syntax.FactoryBody
	Libraries           map[string]*Library

	ForEach lang.Expr

	// Configuration is the explicit library configuration selected by
	// @configuration. Zero means the node uses the default configuration
	// for its own Alias.
	Configuration ConfigRef

	// LockName is the value of a node body's `@lock:` field. Two nodes
	// sharing a non-empty LockName cannot run in parallel under apply's
	// scheduler, even on unrelated DAG branches. Empty means the node is
	// not under a named lock. It applies to any kind; since the
	// scheduler only runs nodes in parallel at apply, a lock has no
	// effect on a data source whose inputs are known and read at plan.
	LockName string

	// Timeout is the parsed value of a node body's `@timeout:` field: a
	// limit on how long the node's apply step may run. Zero means no
	// limit. On expiry the step's context is cancelled and the step
	// fails like any other apply error. Like @lock it only bites at
	// apply, so it does not bound a data source read at plan.
	Timeout time.Duration

	// ConfigurationsRemap is set only on a composite boundary. It maps an
	// inner import alias to the (alias, configuration) of the
	// configuration that backs that import inside the call. The
	// runtime walks the composite chain at lookup time and the
	// validator enforces that the right-hand-side alias matches
	// the key.
	ConfigurationsRemap map[string]ConfigRef
}

// IsComposite reports whether the node is a composite call site (a
// boundary) rather than a primitive leaf. A boundary has its own Kind
// (the call site's resource/data/action kind) just like a leaf; what
// sets it apart is the composite body populated only on boundaries.
func (n *Node) IsComposite() bool {
	return n != nil && n.CompositeSyntaxBody != nil
}

// ConfigRef names the selector and name for one configuration.
type ConfigRef struct {
	Alias string
	Name  string
}

// ExtractSyntaxNodes walks a typed factory or composite body and returns
// every addressable node in source order. The body is assumed to be
// validated.
func ExtractSyntaxNodes(body syntax.FactoryBody, libs map[string]*Library) []*Node {
	return extractSyntaxNodes(body, "", libs)
}

func extractSyntaxNodes(body syntax.FactoryBody, parent string, libs map[string]*Library) []*Node {
	var nodes []*Node
	nodes = append(nodes, extractSyntaxKind(body.Resources, NodeResource, parent, libs)...)
	nodes = append(nodes, extractSyntaxKind(body.Data, NodeData, parent, libs)...)
	nodes = append(nodes, extractSyntaxKind(body.Actions, NodeAction, parent, libs)...)
	if parent == "" {
		nodes = append(nodes, extractSyntaxConfigurations(body.Configurations)...)
		nodes = append(nodes, extractSyntaxOutputs(body.Outputs)...)
	}
	return nodes
}

func extractSyntaxKind(
	decls []syntax.NodeDecl,
	kind NodeKind,
	parent string,
	libs map[string]*Library,
) []*Node {
	out := make([]*Node, 0, len(decls))
	for _, decl := range decls {
		alias := decl.Selector.Alias.Name
		typ := decl.Selector.Export.Name
		name := decl.Name.Name
		addr := composeNameAddress(parent, kind, name)
		if composite := lookupComposite(libs, alias, kind, typ); composite != nil {
			out = append(out, expandSyntaxComposite(addr, parent,
				alias, typ, name, kind, decl.Body, composite, libs)...)
			continue
		}
		node := &Node{
			Address:       addr,
			Kind:          kind,
			Alias:         alias,
			Type:          typ,
			Name:          name,
			Body:          decl.Body,
			Composite:     parent,
			ForEach:       extractForEach(decl.Body),
			Configuration: extractSyntaxConfiguration(decl.Body, alias),
			LockName:      extractLockName(decl.Body),
			Timeout:       extractTimeout(decl.Body),
		}
		out = append(out, node)
	}
	return out
}

// extractLockName reads `@lock: 'name'` from a node body. The value
// must be a string literal; anything else yields the empty string (the
// validator catches the error elsewhere).
func extractLockName(body lang.Expr) string {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return ""
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@lock" {
			continue
		}
		s, ok := fld.Value.(*lang.StringLit)
		if !ok {
			return ""
		}
		return s.Value
	}
	return ""
}

// extractTimeout reads `@timeout: '30s'` from a node body and returns the
// parsed duration, or 0 when the body has none. A non-string value or an
// unparseable duration also yields 0; the validator reports a malformed
// value at compile, so a body that reaches here either has no @timeout or
// a well-formed one.
func extractTimeout(body lang.Expr) time.Duration {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return 0
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@timeout" {
			continue
		}
		s, ok := fld.Value.(*lang.StringLit)
		if !ok {
			return 0
		}
		d, err := time.ParseDuration(s.Value)
		if err != nil {
			return 0
		}
		return d
	}
	return 0
}

// extractForEach returns the iterable expression from a body's
// `@for-each:` field, or nil if the body has none. Non-object bodies
// (which the validator rejects elsewhere) yield nil too.
func extractForEach(body lang.Expr) lang.Expr {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "@for-each" {
			return fld.Value
		}
	}
	return nil
}

func extractSyntaxConfigurationsRemap(body lang.Expr) map[string]ConfigRef {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@configurations" {
			continue
		}
		mapping, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return nil
		}
		out := map[string]ConfigRef{}
		for _, entry := range mapping.Fields {
			if entry.Key.Kind != lang.FieldIdent {
				continue
			}
			if ref, ok := syntaxConfigurationRemap(entry.Key.Name, entry.Value); ok {
				out[entry.Key.Name] = ref
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

func syntaxConfigurationRemap(alias string, expr lang.Expr) (ConfigRef, bool) {
	dp, ok := expr.(*lang.DotPath)
	if !ok || dp.Root == nil || len(dp.Segments) != 1 {
		return ConfigRef{}, false
	}
	if dp.Root.Name != "configuration" {
		return ConfigRef{}, false
	}
	return ConfigRef{Alias: alias, Name: dp.Segments[0].Name}, true
}

func extractSyntaxConfiguration(body lang.Expr, alias string) ConfigRef {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return ConfigRef{}
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@configuration" {
			continue
		}
		dp, ok := fld.Value.(*lang.DotPath)
		if !ok || dp.Root == nil || len(dp.Segments) != 1 {
			return ConfigRef{}
		}
		if dp.Root.Name == "configuration" {
			return ConfigRef{Alias: alias, Name: dp.Segments[0].Name}
		}
		return ConfigRef{}
	}
	return ConfigRef{}
}

func lookupComposite(
	libs map[string]*Library, alias string, kind NodeKind, typ string,
) *CompositeType {
	if libs == nil {
		return nil
	}
	lib, ok := libs[alias]
	if !ok || lib == nil {
		return nil
	}
	composite := lib.Composite(kind, typ)
	if composite == nil || composite.SyntaxBody == nil {
		return nil
	}
	return composite
}

func expandSyntaxComposite(callSiteAddr, parent, alias, typ, name string,
	kind NodeKind, args lang.Expr, composite *CompositeType,
	fallMods map[string]*Library) []*Node {
	scopeMods := composite.Libraries
	if scopeMods == nil {
		scopeMods = fallMods
	}
	out := []*Node{{
		Address:             callSiteAddr,
		Kind:                kind,
		Alias:               alias,
		Type:                typ,
		Name:                name,
		Body:                args,
		Composite:           parent,
		CompositeSyntaxBody: composite.SyntaxBody,
		Libraries:           scopeMods,
		ForEach:             extractForEach(args),
		ConfigurationsRemap: extractSyntaxConfigurationsRemap(args),
	}}
	out = append(out, extractSyntaxNodes(*composite.SyntaxBody, callSiteAddr, scopeMods)...)
	return out
}

func selectorConfigurationAddress(alias, name string) string {
	if name == "default" {
		return "default-configuration." + alias
	}
	return "configuration." + name
}

func configurationNodeAddress(
	nodes map[string]*Node,
	alias string,
	name string,
) (string, bool) {
	addr := selectorConfigurationAddress(alias, name)
	n, ok := nodes[addr]
	if ok && n.Alias == alias && n.Name == name {
		return addr, true
	}
	return "", false
}

// ConfigurationRefNames returns the source-facing names of configuration
// nodes, keyed by configuration name. Defaults are omitted because they
// are selected by selector, not by a configuration.<name> reference.
func ConfigurationRefNames(nodes map[string]*Node) map[string]ConfigRef {
	out := map[string]ConfigRef{}
	ambiguous := map[string]bool{}
	for _, n := range nodes {
		if n.Kind != NodeConfiguration || n.Name == "default" {
			continue
		}
		if ambiguous[n.Name] {
			continue
		}
		if _, exists := out[n.Name]; exists {
			delete(out, n.Name)
			ambiguous[n.Name] = true
			continue
		}
		out[n.Name] = ConfigRef{Alias: n.Alias, Name: n.Name}
	}
	return out
}

// InternalSyntaxConfigurationNames returns the configuration names a typed
// factory body defines internally, keyed by import alias.
func InternalSyntaxConfigurationNames(body syntax.FactoryBody) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, decl := range body.Configurations {
		name := "default"
		if decl.Name != nil {
			name = decl.Name.Name
		}
		addInternalConfigurationName(out, decl.Selector.Name, name)
	}
	return out
}

func addInternalConfigurationName(out map[string]map[string]bool, alias, name string) {
	set := out[alias]
	if set == nil {
		set = map[string]bool{}
		out[alias] = set
	}
	set[name] = true
}

func extractSyntaxConfigurations(decls []syntax.ConfigurationDecl) []*Node {
	out := make([]*Node, 0, len(decls))
	for _, decl := range decls {
		alias := decl.Selector.Name
		name := "default"
		if decl.Name != nil {
			name = decl.Name.Name
		}
		out = append(out, &Node{
			Address: selectorConfigurationAddress(alias, name),
			Kind:    NodeConfiguration,
			Alias:   alias,
			Name:    name,
			Body:    syntaxConfigurationDeclExpr(decl),
		})
	}
	return out
}

func syntaxConfigurationDeclExpr(decl syntax.ConfigurationDecl) lang.Expr {
	if decl.Value != nil {
		return decl.Value
	}
	return decl.Body
}

func extractSyntaxOutputs(decls []syntax.OutputDecl) []*Node {
	out := make([]*Node, 0, len(decls))
	for _, decl := range decls {
		inner := lang.OutputValueExpr(decl.Body)
		if inner == nil {
			continue
		}
		out = append(out, &Node{
			Address: "output." + decl.Name.Name,
			Kind:    NodeOutput,
			Name:    decl.Name.Name,
			Body:    inner,
		})
	}
	return out
}

func composeNameAddress(parent string, kind NodeKind, name string) string {
	return joinAddress(parent, fmt.Sprintf("%s.%s", kind, name))
}

func joinAddress(parent, local string) string {
	if parent == "" {
		return local
	}
	return parent + "/" + local
}
