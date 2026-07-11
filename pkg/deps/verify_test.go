package deps

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type verifyGolden struct {
	Cases []verifyGoldenCase `json:"cases"`
}

type verifyGoldenCase struct {
	Name   string        `json:"name"`
	Result *VerifyResult `json:"result"`
	Error  string        `json:"error,omitempty"`
}

func TestVerifyGolden(t *testing.T) {
	fsys := mapFS(map[string]string{
		ProjectFileName: ubtest.ReadValidFixture(
			t, "testdata/ub/verify", "empty-project"),
		"ub/helloer/library.ub": ubtest.ReadValidFixture(
			t, "testdata/ub/verify", "helloer-library"),
	})
	actualHash := hashProject(t, fsys)
	resolver := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "", "c1"): {Commit: "c1", FS: fsys},
	}}

	result := verifyGolden{}
	for _, test := range []struct {
		name     string
		lock     *ProjectLock
		resolver resolve.Resolver
	}{
		{name: "match", lock: verifyProjectLock(actualHash), resolver: resolver},
		{name: "mismatch", lock: verifyProjectLock("sha256:expected"), resolver: resolver},
		{
			name: "resolve-error",
			lock: verifyProjectLock("sha256:expected"),
			resolver: &fakeResolver{
				sources: map[string]*resolve.Source{},
			},
		},
	} {
		got, err := Verify(test.lock, test.resolver)
		entry := verifyGoldenCase{Name: test.name, Result: got}
		if err != nil {
			entry.Error = err.Error()
		}
		result.Cases = append(result.Cases, entry)
	}

	body, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	body = append(body, '\n')
	want, err := os.ReadFile("testdata/verify.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(body))
}

func verifyProjectLock(hash string) *ProjectLock {
	projectLock := NewProjectLock()
	projectLock.ToolchainVersion = "dev"
	projectLock.Deps["github.com/scratch/repo"] = &ProjectLockDep{
		Kind: ProjectLockKindUB, Version: "v0.8.0", Commit: "c1", Hash: hash,
	}
	projectLock.Deps["github.com/x/golib"] = &ProjectLockDep{
		Kind: ProjectLockKindGo, Version: "v1.0.0", Commit: "go1",
	}
	return projectLock
}
