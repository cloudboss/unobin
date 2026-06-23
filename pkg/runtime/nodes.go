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
	NodeDataSource    NodeKind = "data-source"
	NodeAction        NodeKind = "action"
	NodeOutput        NodeKind = "output"
	NodeLibraryConfig NodeKind = "library-config"
)

// Node is one addressable element of a stack: a single resource instance,
// data source, action, output, or composite call site. Address is the
// dotted form the language uses to reference the node from elsewhere,
// such as `resource.app`, `data-source.image`, `action.deploy`, or
// `output.cluster-arn`.
// Body is the source expression: an ObjectLit for resources, data
// sources, actions, and composites; any Expr for outputs.
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
	LibraryPath         string
	Type                string
	Name                string
	Body                lang.Expr
	Composite           string
	CompositeSyntaxBody *syntax.FactoryBody
	Libraries           map[string]*Library

	ForEach lang.Expr

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
}

// IsComposite reports whether the node is a composite call site (a
// boundary) rather than a primitive leaf. A boundary has its own Kind
// (the call site's resource/data/action kind) just like a leaf; what
// sets it apart is the composite body populated only on boundaries.
func (n *Node) IsComposite() bool {
	return n != nil && n.CompositeSyntaxBody != nil
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
	nodes = append(nodes, extractSyntaxKind(body.Data, NodeDataSource, parent, libs)...)
	nodes = append(nodes, extractSyntaxKind(body.Actions, NodeAction, parent, libs)...)
	nodes = append(nodes, extractSyntaxLibraryConfigs(body.LibraryConfigs, parent)...)
	if parent == "" {
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
		libraryPath := libraryPathForAlias(libs, alias)
		typ := decl.Selector.Export.Name
		name := decl.Name.Name
		addr := composeNameAddress(parent, kind, name)
		if composite := lookupComposite(libs, alias, kind, typ); composite != nil {
			out = append(out, expandSyntaxComposite(addr, parent,
				alias, libraryPath, typ, name, kind, decl.Body, composite, libs)...)
			continue
		}
		node := &Node{
			Address:     addr,
			Kind:        kind,
			Alias:       alias,
			LibraryPath: libraryPath,
			Type:        typ,
			Name:        name,
			Body:        decl.Body,
			Composite:   parent,
			ForEach:     extractForEach(decl.Body),
			LockName:    extractLockName(decl.Body),
			Timeout:     extractTimeout(decl.Body),
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

func libraryPathForAlias(libs map[string]*Library, alias string) string {
	if libs == nil {
		return ""
	}
	lib := libs[alias]
	if lib == nil {
		return ""
	}
	return lib.LibraryPath
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

func expandSyntaxComposite(callSiteAddr, parent, alias, libraryPath, typ, name string,
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
		LibraryPath:         libraryPath,
		Type:                typ,
		Name:                name,
		Body:                args,
		Composite:           parent,
		CompositeSyntaxBody: composite.SyntaxBody,
		Libraries:           scopeMods,
		ForEach:             extractForEach(args),
	}}
	out = append(out, extractSyntaxNodes(*composite.SyntaxBody, callSiteAddr, scopeMods)...)
	return out
}

func libraryConfigNodeAddress(scope string, alias string) string {
	return joinAddress(scope, "library-config."+alias)
}

func libraryConfigNode(
	nodes map[string]*Node,
	scope string,
	alias string,
) (string, bool) {
	addr := libraryConfigNodeAddress(scope, alias)
	n, ok := nodes[addr]
	if ok && n.Kind == NodeLibraryConfig && n.Alias == alias {
		return addr, true
	}
	return "", false
}

func extractSyntaxLibraryConfigs(
	decls []syntax.LibraryConfigDecl,
	parent string,
) []*Node {
	out := make([]*Node, 0, len(decls))
	for _, decl := range decls {
		alias := decl.Alias.Name
		out = append(out, &Node{
			Address:   libraryConfigNodeAddress(parent, alias),
			Kind:      NodeLibraryConfig,
			Alias:     alias,
			Name:      alias,
			Body:      decl.Value,
			Composite: parent,
		})
	}
	return out
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
