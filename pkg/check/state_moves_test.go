package check

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckStateMovesValidatesRootDestinations(t *testing.T) {
	errs := checkSyntaxReferences(t, `
state-moves: [
  { from: 'local.file@resource.old', to: 'local.file@resource.new' },
]
resources: {
  new: local.file { path: 'x', content: 'y' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckStateMovesRejectsMissingRootDestination(t *testing.T) {
	errs := checkSyntaxReferences(t, `
state-moves: [
  { from: 'local.file@resource.old', to: 'local.file@resource.missing' },
]
resources: {
  new: local.file { path: 'x', content: 'y' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.NotEmpty(t, errs.Messages())
	assert.Contains(t, errs.Messages()[0], "destination is not in this factory")
}

func TestCheckStateMovesRejectsWrongSelectorAtDestination(t *testing.T) {
	errs := checkSyntaxReferences(t, `
state-moves: [
  { from: 'local.file@resource.old', to: 'other.file@resource.new' },
]
resources: {
  new: local.file { path: 'x', content: 'y' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.NotEmpty(t, errs.Messages())
	assert.Contains(t, errs.Messages()[0], "destination is not in this factory")
}

func TestCheckStateMovesCollapsesChains(t *testing.T) {
	errs := checkSyntaxReferences(t, `
state-moves: [
  { from: 'local.file@resource.old', to: 'local.file@resource.middle' },
  { from: 'local.file@resource.middle', to: 'local.file@resource.new' },
]
resources: {
  new: local.file { path: 'x', content: 'y' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckStateMovesRejectsMissingChainDestination(t *testing.T) {
	errs := checkSyntaxReferences(t, `
state-moves: [
  { from: 'local.file@resource.old', to: 'local.file@resource.middle' },
  { from: 'local.file@resource.middle', to: 'local.file@resource.final' },
]
resources: {
  new: local.file { path: 'x', content: 'y' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.NotEmpty(t, errs.Messages())
	assert.Contains(t, errs.Messages()[0], "destination is not in this factory")
}

func TestCheckStateMovesValidatesCompositeDestinations(t *testing.T) {
	cluster := parseCheckComposite(t, `
web-cluster: resource {
  state-moves: [
    { from: 'local.file@resource.old', to: 'local.file@resource.new' },
  ]
  resources: {
    new: local.file { path: 'x', content: 'y' }
  }
}
`)
	libs := map[string]*runtime.Library{
		"net": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"web-cluster": {
					Name:       "web-cluster",
					Kind:       runtime.NodeResource,
					SyntaxBody: cluster,
					Libraries:  map[string]*runtime.Library{"local": localFileLibrary()},
				},
			},
		},
		"local": localFileLibrary(),
	}
	errs := checkSyntaxReferences(t, `
resources: {
  app: net.web-cluster {}
}
`, libs)

	require.Empty(t, errs.Messages())
}

func TestCheckStateMovesRejectsMissingCompositeDestination(t *testing.T) {
	cluster := parseCheckComposite(t, `
web-cluster: resource {
  state-moves: [
    { from: 'local.file@resource.old', to: 'local.file@resource.missing' },
  ]
  resources: {
    new: local.file { path: 'x', content: 'y' }
  }
}
`)
	libs := map[string]*runtime.Library{
		"net": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"web-cluster": {
					Name:       "web-cluster",
					Kind:       runtime.NodeResource,
					SyntaxBody: cluster,
					Libraries:  map[string]*runtime.Library{"local": localFileLibrary()},
				},
			},
		},
		"local": localFileLibrary(),
	}
	errs := checkSyntaxReferences(t, `
resources: {
  app: net.web-cluster {}
}
`, libs)

	require.NotEmpty(t, errs.Messages())
	assert.Contains(t, errs.Messages()[0], "destination is not in this factory")
}

func TestCheckStateMovesValidatesCompositeDestinationInsideChild(t *testing.T) {
	child := parseCheckComposite(t, `
cluster: resource {
  resources: {
    new: local.file { path: 'x', content: 'y' }
  }
}
`)
	outer := parseCheckComposite(t, `
web: resource {
  state-moves: [
    { from: 'local.file@resource.old', to: 'local.file@resource.child/resource.new' },
  ]
  resources: {
    child: inner.cluster {}
  }
}
`)
	libs := map[string]*runtime.Library{
		"outer": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"web": {
					Name:       "web",
					Kind:       runtime.NodeResource,
					SyntaxBody: outer,
					Libraries: map[string]*runtime.Library{
						"inner": {
							ResourceComposites: map[string]*runtime.CompositeType{
								"cluster": {
									Name:       "cluster",
									Kind:       runtime.NodeResource,
									SyntaxBody: child,
									Libraries: map[string]*runtime.Library{
										"local": localFileLibrary(),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	errs := checkSyntaxReferences(t, `
resources: {
  app: outer.web {}
}
`, libs)

	require.Empty(t, errs.Messages())
}

func parseCheckComposite(t *testing.T, src string) *syntax.FactoryBody {
	t.Helper()
	f, err := syntax.ParseSource("library.ub", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, f.Library)
	require.Len(t, f.Library.Exports, 1)
	body := f.Library.Exports[0].Body
	return &body
}
