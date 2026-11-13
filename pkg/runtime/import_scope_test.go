package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestCompositeWithoutImportsCannotCallRootFunction(t *testing.T) {
	exec := &Executor{Libraries: map[string]*Library{"root": {
		Functions: map[string]FunctionType{"value": {
			Func: func([]any) (any, error) { return "root", nil },
		}},
	}}}
	expression, err := lang.ParseExpr("scope", []byte("root.value()"))
	require.NoError(t, err)
	for name, libraries := range map[string]map[string]*Library{
		"nil": nil, "empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			scope, err := exec.newEvalScope(&Node{Libraries: libraries}, nil)
			require.NoError(t, err)
			_, err = Eval(expression, scope)
			require.ErrorContains(t, err, `library "root" is not imported`)
		})
	}
}

func TestCompositeLibraryLookupDoesNotUseRootImports(t *testing.T) {
	root := map[string]*Library{"shared": {Name: "root"}}
	local := map[string]*Library{"shared": {Name: "local"}}
	for _, tt := range []struct {
		name     string
		owner    string
		boundary *Node
		want     map[string]*Library
	}{
		{name: "root", want: root},
		{name: "nil", owner: "resource.app", boundary: &Node{}},
		{name: "empty", owner: "resource.app", boundary: &Node{Libraries: map[string]*Library{}},
			want: map[string]*Library{}},
		{name: "missing", owner: "resource.app"},
		{name: "local", owner: "resource.app", boundary: &Node{Libraries: local}, want: local},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dag := &DAG{Nodes: map[string]*Node{}}
			if tt.boundary != nil {
				tt.boundary.CompositeSyntaxBody = &syntax.FactoryBody{}
				dag.Nodes[tt.owner] = tt.boundary
			}
			exec := &Executor{DAG: dag, Libraries: root}
			node := &Node{Composite: tt.owner}
			require.Equal(t, tt.want, exec.librariesFor(node))
			require.Equal(t, tt.want, librariesForNode(dag, root, node))
			analyzer := newSensitivityAnalyzer(nil, root, dag)
			libraries, _ := analyzer.libsForNode(node)
			require.Equal(t, tt.want, libraries)
			require.Equal(t, tt.want, analyzer.scopeFor(tt.owner).libs)
			address := "resource.this"
			if tt.owner != "" {
				address = tt.owner + "/" + address
			}
			require.Equal(t, tt.want, exec.librariesForAddress(address))
		})
	}
}

func TestStateLibraryLookupUsesEnclosingInstance(t *testing.T) {
	owner := instanceAddress("resource.apps", "a/b")
	local := map[string]*Library{"shared": {Name: "local"}}
	exec := &Executor{
		DAG:       &DAG{Nodes: map[string]*Node{}},
		Libraries: map[string]*Library{"shared": {Name: "root"}},
		priorScopeLibraries: map[string]map[string]*Library{
			owner: local,
		},
	}
	require.Equal(t, local, exec.librariesForAddress(owner+"/resource.this"))
	require.Empty(t, exec.librariesForAddress("resource.other/resource.this"))
}

func TestRemovedCompositeWithoutImportsDoesNotInheritParent(t *testing.T) {
	composite := &CompositeType{SyntaxBody: &syntax.FactoryBody{}}
	exec := &Executor{
		DAG: &DAG{Nodes: map[string]*Node{}},
		Libraries: map[string]*Library{"bundle": {
			ResourceComposites: map[string]*CompositeType{"app": composite},
		}},
	}
	snapshot := &state.Snapshot{Entries: []*state.Entry{{
		Address: "resource.app", Category: "resource", Composite: true,
		Binding: &state.Binding{Alias: "bundle", Export: "app"},
	}}}
	rs, err := exec.newStateScopes(snapshot, nil)
	require.NoError(t, err)
	scope, err := exec.ensureCompositeScope(rs, "resource.app")
	require.NoError(t, err)
	require.Empty(t, scope.Libraries)
}
