package runtime

import (
	"fmt"
	"time"

	"github.com/cloudboss/unobin/pkg/lang"
)

// NodeKind tags a Node with its source block.
type NodeKind string

const (
	NodeResource NodeKind = "resource"
	NodeData     NodeKind = "data"
	NodeAction   NodeKind = "action"
	NodeOutput   NodeKind = "output"
)

// Node is one addressable element of a stack: a single resource instance,
// data source, action, output, or composite call site. Address is the
// canonical dotted form the language uses to reference the node from
// elsewhere such as `resource.aws.vpc.main` or `output.cluster-arn`. Body
// is the source expression: an ObjectLit for resources/data/actions/
// composites, any Expr for outputs.
//
// A node inside a composite carries the call site address in
// Composite so the runtime evaluates its body against the composite's
// scope rather than the root. Its address looks like
// `resource.<call site>/<alias>.<type>.<name>`, with the call site as a
// prefix joined by a single `/`. For a composite that itself calls
// another composite the chain continues:
// `resource.<outer>/<inner-rel>/<deepest-rel>`, and each node's
// Composite names its direct enclosing call site.
//
// CompositeBody and Libraries are set only on a composite boundary
// (the call site node), and IsComposite reports that case.
// CompositeBody points to the composite type's full body so the
// runtime can evaluate the `outputs:` block once the internals
// complete. Libraries is the composite's resolved import table; the
// runtime resolves composite-internal node lookups against this map
// rather than the stack root's, so a composite can be reused without
// the caller importing every library it transitively uses.
type Node struct {
	Address       string
	Kind          NodeKind
	Alias         string
	Type          string
	Name          string
	Body          lang.Expr
	Composite     string
	CompositeBody *lang.File
	Libraries     map[string]*Library

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
// sets it apart is the CompositeBody it expands, which extractKind
// populates only on boundaries.
func (n *Node) IsComposite() bool {
	return n.CompositeBody != nil
}

// ConfigRef names a particular configuration on an import.
// `@configuration: aws.east2` parses to {Alias: "aws", Configuration: "east2"}.
type ConfigRef struct {
	Alias         string
	Configuration string
}

// ExtractNodes walks a parsed stack or exported-type file and returns every
// addressable node in source order. The file's shape is assumed to be
// validated. Malformed subtrees are skipped silently rather than reported
// as they should be validated with `lang.ValidateFile` first.
//
// libs is the imported-library table keyed by alias. It is consulted to
// distinguish primitive resource call sites from composite call sites;
// composites expand into a boundary node plus internal nodes. A nil
// or empty libs skips the composite check, in which case every node in
// `resources:` is treated as a primitive.
func ExtractNodes(f *lang.File, libs map[string]*Library) []*Node {
	return extractNodes(f, "", libs)
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
	blocks := topLevelMap(f.Body)
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
		if obj, ok := blocks["outputs"].(*lang.ObjectLit); ok {
			nodes = append(nodes, extractOutputs(obj)...)
		}
	}
	return nodes
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
	for _, alias := range block.Fields {
		if alias.Key.Kind != lang.FieldIdent || alias.Key.IsMeta() {
			continue
		}
		aliasObj, ok := alias.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		for _, t := range aliasObj.Fields {
			if t.Key.Kind != lang.FieldIdent || t.Key.IsMeta() {
				continue
			}
			tObj, ok := t.Value.(*lang.ObjectLit)
			if !ok {
				continue
			}
			composite := lookupComposite(libs, alias.Key.Name, kind, t.Key.Name)
			for _, n := range tObj.Fields {
				if n.Key.Kind != lang.FieldIdent || n.Key.IsMeta() {
					continue
				}
				addr := composeAddress(parent, kind, alias.Key.Name, t.Key.Name, n.Key.Name)
				if composite != nil {
					out = append(out, expandComposite(addr, parent,
						alias.Key.Name, t.Key.Name, n.Key.Name, kind,
						n.Value, composite, libs)...)
					continue
				}
				node := &Node{
					Address:       addr,
					Kind:          kind,
					Alias:         alias.Key.Name,
					Type:          t.Key.Name,
					Name:          n.Key.Name,
					Body:          n.Value,
					Composite:     parent,
					ForEach:       extractForEach(n.Value),
					Configuration: extractConfiguration(n.Value, alias.Key.Name),
					LockName:      extractLockName(n.Value),
					Timeout:       extractTimeout(n.Value),
				}
				out = append(out, node)
			}
		}
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
			dp, ok := entry.Value.(*lang.DotPath)
			if !ok || dp.Root == nil || len(dp.Segments) != 1 {
				continue
			}
			out[entry.Key.Name] = ConfigRef{
				Alias:         dp.Root.Name,
				Configuration: dp.Segments[0].Name,
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// extractConfiguration reads `@configuration: <alias>.<configuration>`
// from a body and returns the configuration segment. The leading alias
// is expected to match the node's own import alias; a mismatch or
// malformed value yields an empty string and the validator reports the
// error elsewhere. An absent meta key returns "" too; the runtime falls
// back to "default" at lookup time.
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
		if dp.Root.Name != alias {
			return ""
		}
		return dp.Segments[0].Name
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
		Libraries:           scopeMods,
		ForEach:             extractForEach(args),
		ConfigurationsRemap: extractConfigurationsRemap(args),
	}}
	out = append(out, extractNodes(composite.Body, callSiteAddr, scopeMods)...)
	return out
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

func topLevelMap(body *lang.ObjectLit) map[string]lang.Expr {
	out := make(map[string]lang.Expr, len(body.Fields))
	for _, f := range body.Fields {
		if f.Key.Kind == lang.FieldIdent && !f.Key.IsMeta() {
			out[f.Key.Name] = f.Value
		}
	}
	return out
}

// composeAddress builds a node's address. Every segment has its own
// kind root: at root it is `<kind>.<alias>.<type>.<name>`, and
// inside a composite it is `<call-site>/<kind>.<alias>.<type>.<name>`. The
// resource, data, and action kinds all follow the same form, so a state
// key reads the same at every depth.
func composeAddress(parent string, kind NodeKind, alias, typ, name string) string {
	if parent == "" {
		return fmt.Sprintf("%s.%s.%s.%s", kind, alias, typ, name)
	}
	return fmt.Sprintf("%s/%s.%s.%s.%s", parent, kind, alias, typ, name)
}
