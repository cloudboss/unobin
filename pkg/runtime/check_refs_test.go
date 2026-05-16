package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func TestCheckReferencesRootScope(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  path: { type: string }
}
resources: {
  local: {
    file: {
      one: {
        path: var.missing
        content: resource.local.file.absent.content
      }
    }
  }
}
outputs: {
  good: resource.local.file.one.path
  bad: data.core.lookup.missing.value
}
`), nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 3)
	require.Contains(t, got[0], `unknown input "missing"`)
	require.Contains(t, got[1], `unknown resource "resource.local.file.absent"`)
	require.Contains(t, got[2], `unknown data "data.core.lookup.missing"`)
}

func TestCheckReferencesResourceModuleMustBeImported(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
resources: {
  greeter: {
    greeting: {
      welcome: {
        message: 'hello'
      }
    }
  }
}
`), map[string]*Module{
		"local": {},
	})

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `module "greeter" is not imported`)
}

func TestCheckReferencesCompositeScope(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  path: { type: string }
}
resources: {
  local: {
    file: {
      one: {
        path: var.path
        content: 'hello'
      }
      two: {
        path: resource.local.file.one.path
        content: 'world'
      }
    }
  }
}
outputs: {
  path: resource.local.file.two.path
}
`)
	mods := map[string]*Module{
		"bundle": {
			Composites: map[string]*CompositeType{
				"file-pair": {
					Name:    "file-pair",
					Body:    composite,
					Modules: map[string]*Module{"local": {}},
				},
			},
		},
	}

	errs := CheckReferences(parseStack(t, `
inputs: {
  target: { type: string }
}
resources: {
  bundle: {
    file-pair: {
      demo: { path: var.target }
    }
  }
}
outputs: {
  path: resource.bundle.file-pair.demo.path
}
`), mods)

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckReferencesCompositeUnknownsUseCompositeScope(t *testing.T) {
	composite := parseStack(t, `
inputs: {
  path: { type: string }
}
resources: {
  local: {
    file: {
      one: {
        path: var.missing
        content: resource.local.file.absent.content
      }
    }
  }
}
outputs: {
  path: resource.local.file.one.path
}
`)
	mods := map[string]*Module{
		"bundle": {
			Composites: map[string]*CompositeType{
				"file-pair": {
					Name:    "file-pair",
					Body:    composite,
					Modules: map[string]*Module{"local": {}},
				},
			},
		},
	}

	errs := CheckReferences(parseStack(t, `
resources: {
  bundle: {
    file-pair: {
      demo: { path: 'x.txt' }
    }
  }
}
`), mods)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 2)
	require.Contains(t, got[0], `unknown input "missing"`)
	require.Contains(t, got[1], `unknown resource "resource.local.file.absent"`)
}

func TestCheckReferencesEachScope(t *testing.T) {
	errs := CheckReferences(parseStack(t, `
inputs: {
  files: { type: list(string) }
}
resources: {
  local: {
    file: {
      many: {
        @for-each: var.files
        path: @each.key
        content: @each.value
      }
      mirror: {
        @for-each: var.files
        path: resource.local.file.many[@each.key].path
        content: @each.value
      }
      bad: {
        path: @each.key
        content: 'no loop'
      }
    }
  }
}
`), nil)

	got := checkRefMessages(t, errs)
	require.Len(t, got, 1)
	require.Contains(t, got[0], `@each is only available inside @for-each`)
}

func checkRefMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	require.NotNil(t, errs)
	var out []string
	for _, err := range errs.Errors() {
		require.Equal(t, lang.ErrResolve, err.Kind)
		out = append(out, err.Msg)
	}
	return out
}
