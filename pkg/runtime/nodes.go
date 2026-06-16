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
// CompositeBody, CompositeSyntaxBody, and Libraries are set only on a
// composite boundary (the call site node), and IsComposite reports that
// case. CompositeBody points to the composite type's generic body.
// CompositeSyntaxBody keeps the typed body for grammar-first code paths.
// Libraries is the composite's resolved import table; the
// runtime resolves composite-internal node lookups against this map
// rather than the stack root's, so a composite can be reused without
// the caller importing every library it transitively uses.
type Node struct {
	Address             string
	Kind                NodeKind
	Alias               string
	Type                string
	Name                string
	Body                lang.Expr
	Composite           string
	CompositeBody       *lang.File
	CompositeSyntaxBody *syntax.FactoryBody
	Libraries           map[string]*Library

	ForEach lang.Expr

	// Configuration names the configuration selected under the node's
	// import (Alias) that the runtime hands to CRUD calls. Empty falls
	// back to "default" at lookup time.
	Configuration string

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
	return n != nil && (n.CompositeBody != nil || n.CompositeSyntaxBody != nil)
}

// ConfigRef names the selector and configuration key for one configuration.
type ConfigRef struct {
	Alias         string
	Configuration string
}

// ExtractNodes is the generic compatibility entrypoint for tests and
// helpers that still construct lang.File bodies directly. Production
// grammar-first callers use ExtractSyntaxNodes.
//
// libs is the imported-library table keyed by alias. It is consulted to
// distinguish primitive resource call sites from composite call sites;
// composites expand into a boundary node plus internal nodes. A nil
// or empty libs skips the composite check, in which case every node in
// `resources:` is treated as a primitive.
func ExtractNodes(f *lang.File, libs map[string]*Library) []*Node {
	return extractNodes(f, "", libs)
}

// ExtractSyntaxNodes walks a typed factory or composite body and returns
// every addressable node in source order. The body is assumed to be
// validated. Composite internals still use each composite's runtime body.
func ExtractSyntaxNodes(body syntax.FactoryBody, libs map[string]*Library) []*Node {
	return extractSyntaxNodes(body, "", libs)
}

// extractNodes is the recursive workhorse. parent is the address of the
// enclosing composite call site, or "" at root; each non-output node
// gets its Composite set to parent, and resource/data/action addresses
// are prefixed with `parent + "/"` when parent is non-empty. Output
// blocks are only emitted at root: a composite's `outputs:` block is
// consumed by `evalCompositeOutputs` at apply time, not turned into
// DAG nodes.
func extractNodes(f *lang.File, parent string, libs map[string]*Library) []*Node {
	if f == nil || f.Body == nil {
		return nil
	}
	var nodes []*Node
	blocks := lang.FieldMap(f.Body)
	if obj, ok := blocks["resources"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractKind(obj, NodeResource, parent, libs)...)
	}
	if obj, ok := blocks["data"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractKind(obj, NodeData, parent, libs)...)
	}
	if obj, ok := blocks["actions"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractKind(obj, NodeAction, parent, libs)...)
	}
	if parent == "" {
		if obj, ok := blocks["configurations"].(*lang.ObjectLit); ok {
			nodes = append(nodes, extractConfigurations(obj)...)
		}
		if obj, ok := blocks["outputs"].(*lang.ObjectLit); ok {
			nodes = append(nodes, extractOutputs(obj)...)
		}
	}
	return nodes
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

// extractKind walks one kind block (resources, data, or actions) and
// returns its nodes. A type that resolves to a composite in the library
// table expands into a boundary node plus its internals; anything else
// becomes a leaf of the given kind. The lookup is kind-keyed, so a
// `data:` call site matches only a data composite and an `actions:` call
// site only an action composite, the same way Go-implemented types are
// placed per kind. A nil libs skips the composite check and every node
// is a leaf. The boundary node takes the call site's kind as its Kind,
// the same as a leaf of that kind; IsComposite tells the two apart by
// the CompositeBody a boundary expands.
func extractKind(
	block *lang.ObjectLit, kind NodeKind, parent string, libs map[string]*Library,
) []*Node {
	var out []*Node
	for _, fld := range block.Fields {
		decl, ok := nodeFieldDecl(fld)
		if !ok {
			continue
		}
		addr := decl.address(parent, kind)
		if composite := lookupComposite(libs, decl.alias, kind, decl.typ); composite != nil {
			out = append(out, expandComposite(addr, parent,
				decl.alias, decl.typ, decl.name, kind, decl.body, composite, libs)...)
			continue
		}
		node := &Node{
			Address:       addr,
			Kind:          kind,
			Alias:         decl.alias,
			Type:          decl.typ,
			Name:          decl.name,
			Body:          decl.body,
			Composite:     parent,
			ForEach:       extractForEach(decl.body),
			Configuration: extractConfiguration(decl.body, decl.alias),
			LockName:      extractLockName(decl.body),
			Timeout:       extractTimeout(decl.body),
		}
		out = append(out, node)
	}
	return out
}

type nodeField struct {
	alias string
	typ   string
	name  string
	body  lang.Expr
	short bool
}

func nodeFieldDecl(fld *lang.Field) (nodeField, bool) {
	if fld.Decl != nil {
		if fld.Decl.Default || fld.Key.Kind != lang.FieldIdent || len(fld.Decl.Selector.Parts) != 2 {
			return nodeField{}, false
		}
		return nodeField{
			alias: fld.Decl.Selector.Parts[0].Name,
			typ:   fld.Decl.Selector.Parts[1].Name,
			name:  fld.Key.Name,
			body:  fld.Decl.Body,
			short: true,
		}, true
	}
	if fld.Key.Kind != lang.FieldPath || len(fld.Key.Path) != 3 {
		return nodeField{}, false
	}
	return nodeField{
		alias: fld.Key.Path[0],
		typ:   fld.Key.Path[1],
		name:  fld.Key.Path[2],
		body:  fld.Value,
	}, true
}

func (d nodeField) address(parent string, kind NodeKind) string {
	if d.short {
		return composeNameAddress(parent, kind, d.name)
	}
	return composeAddress(parent, kind, d.alias, d.typ, d.name)
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

// extractConfigurationsRemap reads `@configurations:` from a
// composite call site body and returns the inner-import-to-outer
// reference map. Entries whose value is not a dotted notation are
// dropped (the parser would reject malformed ones anyway). Entries
// whose right-hand-side alias differs from the key still come
// through so the validator can surface them. An empty or absent
// meta key returns nil.
func extractConfigurationsRemap(body lang.Expr) map[string]ConfigRef {
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
			if ref, ok := configurationRemap(entry.Key.Name, entry.Value); ok {
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

func configurationRemap(alias string, expr lang.Expr) (ConfigRef, bool) {
	dp, ok := expr.(*lang.DotPath)
	if !ok || dp.Root == nil || len(dp.Segments) != 1 {
		return ConfigRef{}, false
	}
	if dp.Root.Name == "configuration" {
		return ConfigRef{Alias: alias, Configuration: dp.Segments[0].Name}, true
	}
	return ConfigRef{Alias: dp.Root.Name, Configuration: dp.Segments[0].Name}, true
}

func syntaxConfigurationRemap(alias string, expr lang.Expr) (ConfigRef, bool) {
	dp, ok := expr.(*lang.DotPath)
	if !ok || dp.Root == nil || len(dp.Segments) != 1 {
		return ConfigRef{}, false
	}
	if dp.Root.Name != "configuration" {
		return ConfigRef{}, false
	}
	return ConfigRef{Alias: alias, Configuration: dp.Segments[0].Name}, true
}

// extractConfiguration reads @configuration from a generic body and returns
// the configuration key. A mismatch or malformed value yields an empty string
// and validation reports the error elsewhere. An absent meta key returns ""
// too; the runtime falls back to "default" at lookup time.
func extractConfiguration(body lang.Expr, alias string) string {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return ""
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@configuration" {
			continue
		}
		dp, ok := fld.Value.(*lang.DotPath)
		if !ok || dp.Root == nil || len(dp.Segments) != 1 {
			return ""
		}
		if dp.Root.Name == "configuration" {
			return dp.Segments[0].Name
		}
		if dp.Root.Name == alias {
			return dp.Segments[0].Name
		}
		return ""
	}
	return ""
}

func extractSyntaxConfiguration(body lang.Expr, _ string) string {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return ""
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@configuration" {
			continue
		}
		dp, ok := fld.Value.(*lang.DotPath)
		if !ok || dp.Root == nil || len(dp.Segments) != 1 {
			return ""
		}
		if dp.Root.Name == "configuration" {
			return dp.Segments[0].Name
		}
		return ""
	}
	return ""
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
	return lib.Composite(kind, typ)
}

// expandComposite emits the boundary node and the internal sub nodes
// for a composite call site. The boundary node sits at the call site
// address and carries the call site args as Body; the runtime
// evaluates the composite's `outputs:` block via CompositeBody once
// the internals complete. parent is the boundary's enclosing call
// site (empty for top-level call sites; the outer call site address
// for nested ones), and is recorded on the boundary's Composite
// field so the boundary's args evaluate against the right scope.
//
// Internals come from a recursive walk of the composite's body with
// the new call site address as the parent prefix, so nested addresses
// build up like `outer/inner-rel/leaf-rel` and each internal's
// Composite names its direct enclosing call site. The composite's
// own Libraries table drives that recursion so a composite that calls
// another composite expands against its own imports rather than the
// caller's. The boundary node also carries the Libraries table itself
// so the runtime can resolve internal node lookups against it. When
// a composite has no Libraries set, fallMods is used as a fallback
// for tests that build composites directly without populating
// imports.
func expandComposite(callSiteAddr, parent, alias, typ, name string,
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
		CompositeBody:       composite.Body,
		CompositeSyntaxBody: composite.SyntaxBody,
		Libraries:           scopeMods,
		ForEach:             extractForEach(args),
		ConfigurationsRemap: extractConfigurationsRemap(args),
	}}
	out = append(out, extractNodes(composite.Body, callSiteAddr, scopeMods)...)
	return out
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
		CompositeBody:       composite.Body,
		CompositeSyntaxBody: composite.SyntaxBody,
		Libraries:           scopeMods,
		ForEach:             extractForEach(args),
		ConfigurationsRemap: extractSyntaxConfigurationsRemap(args),
	}}
	if composite.SyntaxBody != nil {
		out = append(out, extractSyntaxNodes(*composite.SyntaxBody, callSiteAddr, scopeMods)...)
	} else {
		out = append(out, extractNodes(composite.Body, callSiteAddr, scopeMods)...)
	}
	return out
}

func configurationAddress(alias, name string) string {
	return "configuration." + alias + "." + name
}

func selectorConfigurationAddress(alias, name string) string {
	if name == "default" {
		return configurationAddress(alias, name)
	}
	return "configuration." + name
}

func configurationNodeAddress(
	nodes map[string]*Node,
	alias string,
	name string,
) (string, bool) {
	legacy := configurationAddress(alias, name)
	if _, ok := nodes[legacy]; ok {
		return legacy, true
	}
	selector := selectorConfigurationAddress(alias, name)
	n, ok := nodes[selector]
	if ok && n.Alias == alias && n.Name == name {
		return selector, true
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
		out[n.Name] = ConfigRef{Alias: n.Alias, Configuration: n.Name}
	}
	return out
}

// InternalConfigurationNames returns the configuration names a factory
// defines internally, keyed by import alias. The runner consults it
// when loading the stack file so a stack entry cannot collide with a
// name the factory owns.
func InternalConfigurationNames(f *lang.File) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if f == nil || f.Body == nil {
		return out
	}
	block, ok := lang.FieldMap(f.Body)["configurations"].(*lang.ObjectLit)
	if !ok {
		return out
	}
	for _, n := range extractConfigurations(block) {
		addInternalConfigurationName(out, n.Alias, n.Name)
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

// extractConfigurations walks a factory's configurations: block and
// returns one node per defined configuration. Like outputs,
// configurations are defined only at the factory root.
func extractConfigurations(block *lang.ObjectLit) []*Node {
	var out []*Node
	for _, fld := range block.Fields {
		if fld.Decl != nil {
			if n := selectorConfigurationNode(fld); n != nil {
				out = append(out, n)
			}
			continue
		}
		if fld.Key.Kind != lang.FieldPath || len(fld.Key.Path) != 2 {
			continue
		}
		alias, name := fld.Key.Path[0], fld.Key.Path[1]
		out = append(out, &Node{
			Address: configurationAddress(alias, name),
			Kind:    NodeConfiguration,
			Alias:   alias,
			Name:    name,
			Body:    fld.Value,
		})
	}
	return out
}

func selectorConfigurationNode(fld *lang.Field) *Node {
	if len(fld.Decl.Selector.Parts) != 1 {
		return nil
	}
	alias := fld.Decl.Selector.Parts[0].Name
	name := "default"
	if !fld.Decl.Default {
		if fld.Key.Kind != lang.FieldIdent {
			return nil
		}
		name = fld.Key.Name
	}
	return &Node{
		Address: selectorConfigurationAddress(alias, name),
		Kind:    NodeConfiguration,
		Alias:   alias,
		Name:    name,
		Body:    fld.Decl.Body,
	}
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

func extractOutputs(block *lang.ObjectLit) []*Node {
	var out []*Node
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		inner := lang.OutputValueExpr(fld.Value)
		if inner == nil {
			continue
		}
		out = append(out, &Node{
			Address: "output." + fld.Key.Name,
			Kind:    NodeOutput,
			Name:    fld.Key.Name,
			Body:    inner,
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

func composeAddress(parent string, kind NodeKind, alias, typ, name string) string {
	return joinAddress(parent, fmt.Sprintf("%s.%s.%s.%s", kind, alias, typ, name))
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

// InputNames returns the set of input names a file declares.
func InputNames(f *lang.File) map[string]bool {
	names := map[string]bool{}
	if f == nil || f.Body == nil {
		return names
	}
	for name := range lang.FieldMap(lang.TopLevelBlock(f, "inputs")) {
		names[name] = true
	}
	return names
}
