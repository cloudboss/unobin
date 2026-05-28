package runtime

import (
	"slices"
	"sort"

	"github.com/cloudboss/unobin/pkg/lang"
)

// sensitivityAnalyzer decides whether an expression reads any
// sensitive source. Sources are sensitive when their declaration
// says so: stack inputs with `@sensitive: true`, library output
// fields tagged `ub:",sensitive"`, and composite outputs that
// either carry `@sensitive: true` on the wrapper or propagate from
// a sensitive source themselves.
//
// Per-composite analysis is memoized so a stack with many call
// sites of the same composite type pays the inference cost once.
type sensitivityAnalyzer struct {
	rootInputs map[string]bool
	rootLocals map[string]lang.Expr
	rootMods   map[string]*Library
	dag        *DAG
	cache      map[*lang.File]*compositeSensitivity
}

type compositeSensitivity struct {
	inputs  map[string]bool
	outputs map[string]bool
}

// sensScope bundles what a body's references resolve against while
// deciding sensitivity: the sensitive input names, the library table,
// the scope's `locals:` declarations (so a `local.X` can be followed
// to its expression), and a guard set that breaks cyclic locals.
type sensScope struct {
	vars    map[string]bool
	libs    map[string]*Library
	locals  map[string]lang.Expr
	forcing map[string]bool
}

func newSensScope(
	vars map[string]bool, libs map[string]*Library, locals map[string]lang.Expr,
) *sensScope {
	return &sensScope{
		vars:    vars,
		libs:    libs,
		locals:  locals,
		forcing: map[string]bool{},
	}
}

func newSensitivityAnalyzer(
	rootSource *lang.File, rootMods map[string]*Library, dag *DAG,
) *sensitivityAnalyzer {
	return &sensitivityAnalyzer{
		rootInputs: inputsBlockSensitive(rootSource),
		rootLocals: localExprs(localsBlock(rootSource)),
		rootMods:   rootMods,
		dag:        dag,
		cache:      map[*lang.File]*compositeSensitivity{},
	}
}

// sensitiveInputs walks every field of an object-literal body and
// returns the kebab-case field names whose value expression reads
// from any sensitive source. The body's enclosing scope is named
// by compositeAddr: empty for the root, or the template address of
// a composite call site for an internal node.
func (s *sensitivityAnalyzer) sensitiveInputs(body lang.Expr, compositeAddr string) []string {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil
	}
	sc := s.scopeFor(compositeAddr)
	var names []string
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		if s.exprSensitive(fld.Value, sc) {
			names = append(names, fld.Key.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return names
}

// sensitiveOutputs returns the kebab-case output field names this
// node exposes as sensitive. For a primitive resource, data source,
// or action it comes from the library schema's tagged fields; for a
// composite call site it comes from the composite type's analyzed
// outputs (declared `@sensitive` plus propagation).
func (s *sensitivityAnalyzer) sensitiveOutputs(n *Node) []string {
	switch n.Kind {
	case NodeResource, NodeAction, NodeData:
		libs, _ := s.libsForNode(n)
		lib, ok := libs[n.Alias]
		if !ok || lib == nil || lib.Schema == nil {
			return nil
		}
		var ts *TypeSchema
		switch n.Kind {
		case NodeResource:
			ts = lib.Schema.Resources[n.Type]
		case NodeData:
			ts = lib.Schema.DataSources[n.Type]
		case NodeAction:
			ts = lib.Schema.Actions[n.Type]
		}
		if ts == nil {
			return nil
		}
		return append([]string(nil), ts.SensitiveOutputs...)
	case NodeComposite:
		cs := s.compositeSensitivity(n)
		if cs == nil {
			return nil
		}
		names := make([]string, 0, len(cs.outputs))
		for name := range cs.outputs {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}
	return nil
}

// libsForNode returns the libraries table that resolves the node's
// import alias. Root nodes use the analyzer's rootMods; nodes
// inside a composite use the call-site boundary's Libraries.
func (s *sensitivityAnalyzer) libsForNode(n *Node) (map[string]*Library, *Node) {
	if n.Composite == "" || s.dag == nil {
		return s.rootMods, nil
	}
	tmpl, _ := splitInstanceAddress(n.Composite)
	boundary, ok := s.dag.Nodes[tmpl]
	if !ok || boundary.Libraries == nil {
		return s.rootMods, boundary
	}
	return boundary.Libraries, boundary
}

// scopeFor returns the sensitive-vars set and libraries table to
// resolve references against when analyzing inside the named
// composite call site. The root scope returns the analyzer's
// rootInputs and rootMods.
func (s *sensitivityAnalyzer) scopeFor(compositeAddr string) *sensScope {
	if compositeAddr == "" || s.dag == nil {
		return newSensScope(s.rootInputs, s.rootMods, s.rootLocals)
	}
	tmpl, _ := splitInstanceAddress(compositeAddr)
	boundary, ok := s.dag.Nodes[tmpl]
	if !ok {
		return newSensScope(s.rootInputs, s.rootMods, s.rootLocals)
	}
	cs := s.compositeSensitivity(boundary)
	if cs == nil {
		return newSensScope(s.rootInputs, s.rootMods, s.rootLocals)
	}
	libs := boundary.Libraries
	if libs == nil {
		libs = s.rootMods
	}
	return newSensScope(cs.inputs, libs, localExprs(localsBlock(boundary.CompositeBody)))
}

// compositeSensitivity returns the analyzed sensitivity facts for
// a composite call site's type: which of its declared inputs are
// sensitive, and which of its outputs are sensitive after merging
// declared `@sensitive: true` wrappers with propagation from
// sensitive sources. Results are cached on the body file pointer
// because every call site of the same composite type shares its
// body.
func (s *sensitivityAnalyzer) compositeSensitivity(boundary *Node) *compositeSensitivity {
	body := boundary.CompositeBody
	if body == nil {
		return nil
	}
	if cached, ok := s.cache[body]; ok {
		return cached
	}
	cs := &compositeSensitivity{
		inputs:  inputsBlockSensitive(body),
		outputs: map[string]bool{},
	}
	s.cache[body] = cs
	if body.Body == nil {
		return cs
	}
	outputs, ok := topLevelMap(body.Body)["outputs"].(*lang.ObjectLit)
	if !ok {
		return cs
	}
	for name := range lang.SensitiveOutputs(outputs) {
		cs.outputs[name] = true
	}
	libs := boundary.Libraries
	if libs == nil {
		libs = s.rootMods
	}
	sc := newSensScope(cs.inputs, libs, localExprs(localsBlock(body)))
	for _, fld := range outputs.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		name := fld.Key.Name
		if cs.outputs[name] {
			continue
		}
		inner := lang.OutputValueExpr(fld.Value)
		if inner == nil {
			continue
		}
		if s.exprSensitive(inner, sc) {
			cs.outputs[name] = true
		}
	}
	return cs
}

// exprSensitive walks an expression and returns true on the first
// dot path that points to a sensitive source under the given scope.
func (s *sensitivityAnalyzer) exprSensitive(e lang.Expr, sc *sensScope) bool {
	if e == nil {
		return false
	}
	sensitive := false
	lang.Walk(e, func(node lang.Expr) {
		if sensitive {
			return
		}
		dp, ok := node.(*lang.DotPath)
		if !ok {
			return
		}
		if s.dotPathSensitive(dp, sc) {
			sensitive = true
		}
	})
	return sensitive
}

func (s *sensitivityAnalyzer) dotPathSensitive(dp *lang.DotPath, sc *sensScope) bool {
	switch dp.Root.Name {
	case "var":
		if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
			return false
		}
		return sc.vars[dp.Segments[0].Name]
	case "local":
		return s.localSensitive(dp, sc)
	case "resource", "data", "action":
		if len(dp.Segments) < 4 {
			return false
		}
		alias := dp.Segments[0].Name
		typ := dp.Segments[1].Name
		field := trailingNamedSegment(dp)
		if alias == "" || typ == "" || field == "" {
			return false
		}
		lib, ok := sc.libs[alias]
		if !ok || lib == nil {
			return false
		}
		if dp.Root.Name == "resource" {
			if comp, ok := lib.Composites[typ]; ok {
				return s.compositeTypeOutputs(comp)[field]
			}
		}
		if lib.Schema == nil {
			return false
		}
		var ts *TypeSchema
		switch dp.Root.Name {
		case "resource":
			ts = lib.Schema.Resources[typ]
		case "data":
			ts = lib.Schema.DataSources[typ]
		case "action":
			ts = lib.Schema.Actions[typ]
		}
		if ts == nil {
			return false
		}
		if slices.Contains(ts.SensitiveOutputs, field) {
			return true
		}
	}
	return false
}

// localSensitive reports whether a `local.<name>` reference reads a
// sensitive source. Only the sub-expressions the navigation actually
// reads are analyzed, so reading a non-sensitive field of a local is
// not masked just because a sibling field is sensitive; see
// narrowLocal. Each sub-expression is analyzed in the same scope,
// following one local into another. The scope's guard set stops a
// cyclic locals block from looping; such a cycle is a compile error
// reported elsewhere.
func (s *sensitivityAnalyzer) localSensitive(dp *lang.DotPath, sc *sensScope) bool {
	if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
		return false
	}
	name := dp.Segments[0].Name
	if sc.forcing[name] {
		return false
	}
	expr, ok := sc.locals[name]
	if !ok {
		return false
	}
	sc.forcing[name] = true
	defer delete(sc.forcing, name)
	for _, narrowed := range narrowLocal(expr, dp.Segments[1:]) {
		if s.exprSensitive(narrowed, sc) {
			return true
		}
	}
	return false
}

// compositeTypeOutputs returns the sensitive output names of a
// composite type registered as a CompositeType (not via a DAG call
// site). Used when a body expression references
// `resource.<alias>.<composite-type>.<name>.<field>` and we need
// to know whether <field> is sensitive on that composite.
func (s *sensitivityAnalyzer) compositeTypeOutputs(ct *CompositeType) map[string]bool {
	if ct == nil || ct.Body == nil {
		return nil
	}
	if cached, ok := s.cache[ct.Body]; ok {
		return cached.outputs
	}
	cs := &compositeSensitivity{
		inputs:  inputsBlockSensitive(ct.Body),
		outputs: map[string]bool{},
	}
	s.cache[ct.Body] = cs
	if ct.Body.Body == nil {
		return cs.outputs
	}
	outputs, ok := topLevelMap(ct.Body.Body)["outputs"].(*lang.ObjectLit)
	if !ok {
		return cs.outputs
	}
	for name := range lang.SensitiveOutputs(outputs) {
		cs.outputs[name] = true
	}
	libs := ct.Libraries
	if libs == nil {
		libs = s.rootMods
	}
	sc := newSensScope(cs.inputs, libs, localExprs(localsBlock(ct.Body)))
	for _, fld := range outputs.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		name := fld.Key.Name
		if cs.outputs[name] {
			continue
		}
		inner := lang.OutputValueExpr(fld.Value)
		if inner == nil {
			continue
		}
		if s.exprSensitive(inner, sc) {
			cs.outputs[name] = true
		}
	}
	return cs.outputs
}

// inputsBlockSensitive returns the set of input names declared
// `@sensitive: true` in a file's top-level `inputs:` block.
// Returns an empty map when the file or block is absent.
func inputsBlockSensitive(f *lang.File) map[string]bool {
	if f == nil || f.Body == nil {
		return map[string]bool{}
	}
	inputs, ok := topLevelMap(f.Body)["inputs"].(*lang.ObjectLit)
	if !ok {
		return map[string]bool{}
	}
	return lang.SensitiveInputs(inputs)
}

// trailingNamedSegment returns the last named segment of a dot
// path past the three-segment node address. Index-only segments
// at the tail are skipped so `resource.alias.t.name['k'].field`
// returns "field". Returns "" when no trailing named segment
// exists.
func trailingNamedSegment(dp *lang.DotPath) string {
	for i := len(dp.Segments) - 1; i >= 3; i-- {
		if dp.Segments[i].Name != "" {
			return dp.Segments[i].Name
		}
	}
	return ""
}
