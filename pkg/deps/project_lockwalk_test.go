package deps

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func projectLockWalkFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/project-lockwalk", name)
}

func mapFS(files map[string]string) fstest.MapFS {
	m := make(fstest.MapFS, len(files))
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

// goSrc is a fetched source that classifies as a Go library (a .go file,
// no .ub files).
func goSrc(commit string) *resolve.Source {
	return &resolve.Source{Commit: commit, FS: mapFS(map[string]string{"lib.go": "package lib"})}
}

// ubSrc is a fetched UB-library source; its .ub files hold whatever
// imports the test needs for recursion.
func ubSrc(commit, _ string, files map[string]string) *resolve.Source {
	return &resolve.Source{Commit: commit, FS: mapFS(files)}
}

func TestProjectLockFromImportsRemoteGoLibrary(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "remote-go-root"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/core", "v0.1.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: "v0.1.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/cloudboss/unobin//pkg/libraries/core": {
			Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsSourceDeclaredFactory(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-source-declared-factory"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/core", "v0.1.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: "v0.1.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/cloudboss/unobin//pkg/libraries/core": {
			Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsValidatesSourceDeclaredFactory(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-validates-source-declared-factory"),
	})

	_, err := ProjectLockFromImports(root, map[Dependency]string{}, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `library "text" is not imported`)
}

func TestProjectLockFromImportsRejectsUntypedUBFile(t *testing.T) {
	root := fstest.MapFS{
		"loose.ub": {Data: []byte(projectLockWalkFixture(t, "project-lock-from-imports-rejects-untyped-ub-file"))},
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "", "v1.0.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{{URL: "github.com/x/y"}: "v1.0.0"}

	_, err := ProjectLockFromImports(root, sel, r, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine UB file role")
}

func TestProjectLockFromImportsRejectsGrammarFirstFactoryImport(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-rejects-grammar-first-factory-import-1"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/app", "", "v0.1.0"): ubSrc("c1", "h1", map[string]string{
			"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-rejects-grammar-first-factory-import-2"),
		}),
	}}
	sel := map[Dependency]string{{URL: "example.com/app"}: "v0.1.0"}

	_, err := ProjectLockFromImports(root, sel, r, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be imported")
}

func TestProjectLockFromImportsSkipsReplaced(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-skips-replaced"),
	})
	// The replaced repo resolves locally (no version), and it's a Go library.
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/cloudboss/unobin-library-aws", "", ""): goSrc("local"),
	}}
	replace := map[Dependency]string{
		{URL: "github.com/cloudboss/unobin-library-aws"}: "../../../..",
	}
	// Empty selection: a replaced dependency needs no floor.
	projectLock, err := ProjectLockFromImports(root, map[Dependency]string{}, r, replace)
	require.NoError(t, err)
	assert.Empty(t, projectLock.Deps)
}

func TestProjectLockFromImportsReplacedUBLocksTransitive(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-replaced-ub-locks-transitive-1"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/me/mylib", "", ""): ubSrc("local", "", map[string]string{
			"library.ub": projectLockWalkFixture(t, "project-lock-from-imports-replaced-ub-locks-transitive-2"),
		}),
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/core", "v0.1.0"): goSrc("c1"),
	}}
	replace := map[Dependency]string{{URL: "github.com/me/mylib"}: "../mylib"}
	sel := map[Dependency]string{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/core"}: "v0.1.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, replace)
	require.NoError(t, err)
	// The replaced library is not written to project-lock; its transitive remote dep is.
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/cloudboss/unobin//pkg/libraries/core": {
			Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsLibraryProject(t *testing.T) {
	// A library project has no factory.ub; its imports live in source-declared
	// composite files at the project root.
	root := mapFS(map[string]string{
		"library.ub": projectLockWalkFixture(t, "project-lock-from-imports-library-project"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.1.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: "v0.1.0"}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsMultiLibraryRepo(t *testing.T) {
	// A repo whose libraries live in subdirectories, with no factory.ub or
	// root-level body files; the walk must descend into the subdirs.
	root := mapFS(map[string]string{
		"ub/helloer/library.ub": projectLockWalkFixture(t, "project-lock-from-imports-multi-library-repo"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/local", "v0.5.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/local"}: "v0.5.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/cloudboss/unobin//pkg/libraries/local": {
			Kind: ProjectLockKindGo, Version: "v0.5.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsRemoteSubdirLibraryFiles(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-remote-subdir-library-files-1"),
	})
	packageFiles := map[string]string{
		"resource-hello.ub": projectLockWalkFixture(t, "project-lock-from-imports-remote-subdir-library-files-2"),
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "ub/helloer/v0.8.0"): ubSrc(
			"c1", "sha256:h1", packageFiles),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: "v0.8.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind:    ProjectLockKindUB,
			Version: "v0.8.0",
			Commit:  "c1",
			Hash:    hashProject(t, mapFS(packageFiles)),
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsPackageImportLocksOwningProject(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "package-owning-root"),
	})
	projectFiles := map[string]string{
		ProjectFileName:                projectLockWalkFixture(t, "package-owning-project"),
		"ub/helloer/resource-hello.ub": projectLockWalkFixture(t, "package-owning-project-lib"),
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.8.0"): ubSrc(
			"c1", "sha256:package", map[string]string{
				"resource-hello.ub": projectLockWalkFixture(t, "package-owning-package-lib"),
			}),
		srcKey("github.com/scratch/repo", "", "v0.8.0"): ubSrc(
			"c1", "sha256:project", projectFiles),
	}}
	sel := map[Dependency]string{{URL: "github.com/scratch/repo"}: "v0.8.0"}

	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo": {
			Kind:    ProjectLockKindUB,
			Version: "v0.8.0",
			Commit:  "c1",
			Hash:    hashProject(t, mapFS(projectFiles)),
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsRejectsParentProjectPastNestedMarker(t *testing.T) {
	projectFS := mapFS(map[string]string{
		ProjectFileName:                      projectLockWalkFixture(t, "parent-project-root-project"),
		"ub/project-b/project.ub":            projectLockWalkFixture(t, "parent-project-nested-project"),
		"ub/project-b/comprehensions/lib.ub": "hello: resource {}\n",
	})
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "reject-parent-project-import"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/repo", "ub/project-b/comprehensions", "v1.0.0"): {
			Commit:        "c1",
			FS:            mapFS(map[string]string{"lib.ub": "hello: resource {}\n"}),
			ProjectFS:     projectFS,
			PackageSubdir: "ub/project-b/comprehensions",
		},
		srcKey("example.com/repo", "", "v1.0.0"): {Commit: "c1", FS: projectFS},
	}}
	sel := map[Dependency]string{{URL: "example.com/repo"}: "v1.0.0"}

	_, err := ProjectLockFromImports(root, sel, r, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not own package")
	assert.Contains(t, err.Error(), "example.com/repo//ub/project-b")
}

func TestProjectLockFromImportsNestedPackageImportLocksOwningProject(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "nested-package-root"),
	})
	projectFiles := map[string]string{
		ProjectFileName:             projectLockWalkFixture(t, "nested-package-project"),
		"comprehensions/library.ub": "hello: resource { description: 'hi' }\n",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/project-b/comprehensions",
			"ub/project-b/v0.1.0"): ubSrc("c1", "sha256:package", map[string]string{
			"library.ub": "hello: resource { outputs: { message: { value: 'hi' } } }\n",
		}),
		srcKey("github.com/scratch/repo", "ub/project-b", "ub/project-b/v0.1.0"): ubSrc(
			"c1", "sha256:project", projectFiles),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/scratch/repo", Subdir: "ub/project-b"}: "v0.1.0",
	}

	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo//ub/project-b": {
			Kind:    ProjectLockKindUB,
			Version: "v0.1.0",
			Commit:  "c1",
			Hash:    hashProject(t, mapFS(projectFiles)),
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsReportsPackageMissingFromSelectedProject(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "missing-selected-package-root"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/project-b/comprehensions", "v0.8.0"): {
			Commit: "c1",
			FS:     fstest.MapFS{},
		},
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.8.0"): ubSrc(
			"c1", "sha256:package", map[string]string{
				"resource-hello.ub": projectLockWalkFixture(t, "missing-selected-package-lib"),
			}),
	}}
	sel := map[Dependency]string{{URL: "github.com/scratch/repo"}: "v0.8.0"}

	_, err := ProjectLockFromImports(root, sel, r, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selected project github.com/scratch/repo")
	assert.Contains(t, err.Error(), "does not provide package")
	assert.Contains(t, err.Error(), "add the owning project")
}

func TestProjectLockFromImportsSkipsNestedProjects(t *testing.T) {
	root := mapFS(map[string]string{
		ProjectFileName:                projectLockWalkFixture(t, "project-lock-from-imports-skips-nested-projects-1"),
		"factory-a/factory.ub":         projectLockWalkFixture(t, "project-lock-from-imports-skips-nested-projects-2"),
		"library-c/" + ProjectFileName: projectLockWalkFixture(t, "project-lock-from-imports-skips-nested-projects-3"),
		"library-c/abc.ub":             projectLockWalkFixture(t, "project-lock-from-imports-skips-nested-projects-4"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/shared", "lib", "lib/v1.0.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{{URL: "example.com/shared", Subdir: "lib"}: "v1.0.0"}

	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"example.com/shared//lib": {
			Kind: ProjectLockKindGo, Version: "v1.0.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsScansNestedProjectWhenStartedThere(t *testing.T) {
	root := mapFS(map[string]string{
		ProjectFileName: projectLockWalkFixture(t, "scan-nested-start-project"),
		"abc.ub":        projectLockWalkFixture(t, "scan-nested-start-library"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/nested", "lib", "lib/v1.0.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{{URL: "example.com/nested", Subdir: "lib"}: "v1.0.0"}

	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"example.com/nested//lib": {
			Kind: ProjectLockKindGo, Version: "v1.0.0", Commit: "c1",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsRejectsInvalidNestedProject(t *testing.T) {
	root := mapFS(map[string]string{
		ProjectFileName:                projectLockWalkFixture(t, "invalid-nested-root-project"),
		"factory.ub":                   projectLockWalkFixture(t, "invalid-nested-root-factory"),
		"library-c/" + ProjectFileName: projectLockWalkFixture(t, "invalid-nested-child-project"),
	})

	_, err := ProjectLockFromImports(root, map[Dependency]string{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project")
}

func TestProjectLockFromImportsRecursesThroughRemoteUB(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-recurses-through-remote-ub-1"),
	})
	packageFiles := map[string]string{
		"library.ub": projectLockWalkFixture(t, "project-lock-from-imports-recurses-through-remote-ub-2"),
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.1.0"): ubSrc(
			"c2", "h2", packageFiles),
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/local", "v0.1.0"): goSrc("c3"),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}:              "v0.1.0",
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/local"}: "v0.1.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind:    ProjectLockKindUB,
			Version: "v0.1.0",
			Commit:  "c2",
			Hash:    hashProject(t, mapFS(packageFiles)),
		},
		"github.com/cloudboss/unobin//pkg/libraries/local": {
			Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "c3",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsRecursesThroughSourceDeclaredRemoteUB(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "source-remote-root"),
	})
	packageFiles := map[string]string{
		"library.ub": projectLockWalkFixture(t, "source-remote-library"),
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.1.0"): ubSrc(
			"c2", "h2", packageFiles),
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/local", "v0.1.0"): goSrc("c3"),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}:              "v0.1.0",
		{URL: "github.com/cloudboss/unobin", Subdir: "pkg/libraries/local"}: "v0.1.0",
	}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind:    ProjectLockKindUB,
			Version: "v0.1.0",
			Commit:  "c2",
			Hash:    hashProject(t, mapFS(packageFiles)),
		},
		"github.com/cloudboss/unobin//pkg/libraries/local": {
			Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "c3",
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsFollowsLocalWithoutLocking(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub":         projectLockWalkFixture(t, "project-lock-from-imports-follows-local-without-locking-1"),
		"greeter/library.ub": projectLockWalkFixture(t, "project-lock-from-imports-follows-local-without-locking-2"),
	})
	packageFiles := map[string]string{"library.ub": projectLockWalkFixture(t, "local-follow-package-lib")}
	r := &fakeResolver{
		sources: map[string]*resolve.Source{
			srcKey("github.com/scratch/repo", "ub/helloer", "v0.1.0"): ubSrc(
				"c2", "h2", packageFiles),
		},
		locals: map[string]*resolve.Source{
			"./greeter": ubSrc("", "", map[string]string{
				"library.ub": "greeting: resource { description: 'greeter' }\n",
			}),
		},
	}
	sel := map[Dependency]string{{URL: "github.com/scratch/repo", Subdir: "ub/helloer"}: "v0.1.0"}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind:    ProjectLockKindUB,
			Version: "v0.1.0",
			Commit:  "c2",
			Hash:    hashProject(t, mapFS(packageFiles)),
		},
	}, projectLock.Deps)
}

func TestProjectLockFromImportsRejectsLocalGoImport(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-rejects-local-go-import"),
	})
	r := &fakeResolver{locals: map[string]*resolve.Source{
		"../../../..": {FS: mapFS(map[string]string{
			"go.mod": "module github.com/cloudboss/unobin-library-aws\n\ngo 1.26\n",
		})},
	}}
	_, err := ProjectLockFromImports(root, map[Dependency]string{}, r, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		"is a Go library (module github.com/cloudboss/unobin-library-aws)")
	assert.Contains(t, err.Error(), "in project.ub:")
}

func TestProjectLockFromImportsRejectsLocalImportIntoDifferentProject(t *testing.T) {
	root := mapFS(map[string]string{
		ProjectFileName:                projectLockWalkFixture(t, "local-different-root-project"),
		"factory.ub":                   projectLockWalkFixture(t, "local-different-root-factory"),
		"library-c/" + ProjectFileName: projectLockWalkFixture(t, "local-different-child-project"),
		"library-c/library.ub":         projectLockWalkFixture(t, "local-different-child-library"),
	})
	r := &fakeResolver{locals: map[string]*resolve.Source{
		"./library-c": ubSrc("", "", map[string]string{
			"library.ub": "hello: resource { description: 'hello' }\n",
		}),
	}}

	_, err := ProjectLockFromImports(root, map[Dependency]string{}, r, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different project")
	assert.Contains(t, err.Error(), "project.replace")
}

func TestProjectLockFromImportsDedups(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-dedups"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "v1.0.0"): goSrc("c"),
	}}
	sel := map[Dependency]string{{URL: "github.com/x/y", Subdir: "lib"}: "v1.0.0"}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Len(t, projectLock.Deps, 1)
}

func TestProjectLockFromImportsRejectsGoModuleMajorMismatch(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-rejects-go-module-major-mismatch"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/lib", "", "v2.0.0"): {
			Commit:       "c2",
			FS:           mapFS(map[string]string{"lib.go": "package lib\n"}),
			ModulePath:   "example.com/lib",
			GoImportPath: "example.com/lib",
		},
	}}
	sel := map[Dependency]string{{URL: "example.com/lib"}: "v2.0.0"}

	_, err := ProjectLockFromImports(root, sel, r, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "module path must end in /v2")
}

func TestProjectLockFromImportsUsesSelectionVersion(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-uses-selection-version"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "lib/v2.0.0"): goSrc("c2"),
	}}
	sel := map[Dependency]string{{URL: "github.com/x/y", Subdir: "lib"}: "v2.0.0"}
	projectLock, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", projectLock.Deps["github.com/x/y//lib"].Version)
	assert.Equal(t, "lib/v2.0.0", r.lastRef.Version)
}

func TestProjectLockFromImportsUsesPlainTagForRootProject(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-uses-plain-tag-for-root-project"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "", "v2.0.0"): goSrc("c2"),
	}}
	sel := map[Dependency]string{{URL: "github.com/x/y"}: "v2.0.0"}

	_, err := ProjectLockFromImports(root, sel, r, nil)
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", r.lastRef.Version)
}

func TestProjectLockFromImportsRejectsRepoWithoutFloor(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-rejects-repo-without-floor"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "v1.0.0"): goSrc("c1"),
	}}
	// Empty selection: nothing covers github.com/x/y.
	_, err := ProjectLockFromImports(root, map[Dependency]string{}, r, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.com/x/y")
	assert.Contains(t, err.Error(), "deps get")
}

func TestProjectLockFromImportsDetectsCycle(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "project-lock-from-imports-detects-cycle-1"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/a", "lib", "v1.0.0"): ubSrc("ca", "ha", map[string]string{
			"library.ub": projectLockWalkFixture(t, "project-lock-from-imports-detects-cycle-2"),
		}),
		srcKey("github.com/x/b", "lib", "v1.0.0"): ubSrc("cb", "hb", map[string]string{
			"library.ub": projectLockWalkFixture(t, "project-lock-from-imports-detects-cycle-3"),
		}),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/x/a", Subdir: "lib"}: "v1.0.0",
		{URL: "github.com/x/b", Subdir: "lib"}: "v1.0.0",
	}
	_, err := ProjectLockFromImports(root, sel, r, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}
