package deps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectContains(t *testing.T) {
	tests := []struct {
		name    string
		project ProjectID
		pkg     RemotePackage
		want    string
		ok      bool
	}{
		{
			name:    "different url",
			project: ProjectID{URL: "github.com/acme/repo"},
			pkg:     RemotePackage{URL: "github.com/acme/other", Subdir: "ub/helloer"},
		},
		{
			name:    "root project owns root package",
			project: ProjectID{URL: "github.com/acme/repo"},
			pkg:     RemotePackage{URL: "github.com/acme/repo"},
			want:    ".",
			ok:      true,
		},
		{
			name:    "root project owns child package",
			project: ProjectID{URL: "github.com/acme/repo"},
			pkg:     RemotePackage{URL: "github.com/acme/repo", Subdir: "ub/helloer"},
			want:    "ub/helloer",
			ok:      true,
		},
		{
			name:    "nested project owns itself",
			project: ProjectID{URL: "github.com/acme/repo", Subdir: "ub/project-b"},
			pkg:     RemotePackage{URL: "github.com/acme/repo", Subdir: "ub/project-b"},
			want:    ".",
			ok:      true,
		},
		{
			name:    "nested project owns child package",
			project: ProjectID{URL: "github.com/acme/repo", Subdir: "ub/project-b"},
			pkg: RemotePackage{
				URL:    "github.com/acme/repo",
				Subdir: "ub/project-b/comprehensions",
			},
			want: "comprehensions",
			ok:   true,
		},
		{
			name:    "sibling prefix does not match",
			project: ProjectID{URL: "github.com/acme/repo", Subdir: "ub/project-b"},
			pkg:     RemotePackage{URL: "github.com/acme/repo", Subdir: "ub/project-bad"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ProjectContains(tt.project, tt.pkg)
			require.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMostSpecificProject(t *testing.T) {
	projects := []ProjectID{
		{URL: "github.com/acme/repo"},
		{URL: "github.com/acme/repo", Subdir: "ub/project-b"},
		{URL: "github.com/acme/repo", Subdir: "ub/project-b/comprehensions"},
	}
	owner, ok := MostSpecificProject(projects, RemotePackage{
		URL:    "github.com/acme/repo",
		Subdir: "ub/project-b/comprehensions/actions",
	})
	require.True(t, ok)
	assert.Equal(t,
		ProjectID{URL: "github.com/acme/repo", Subdir: "ub/project-b/comprehensions"},
		owner.Project)
	assert.Equal(t, "actions", owner.PackageSubdir)
}

func TestProjectTag(t *testing.T) {
	assert.Equal(t, "v1.2.3", ProjectTag(ProjectID{URL: "github.com/acme/repo"}, "v1.2.3"))
	assert.Equal(t, "ub/project-b/v1.2.3", ProjectTag(
		ProjectID{URL: "github.com/acme/repo", Subdir: "ub/project-b"}, "v1.2.3"))
}
