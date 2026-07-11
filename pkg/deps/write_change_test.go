package deps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/filechange"
)

type dependencyWriteGolden struct {
	Changes []filechange.Change `json:"changes"`
}

func TestDependencyWriteChangesGolden(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, ProjectFileName)
	lockPath := filepath.Join(dir, ProjectLockFileName)
	project := &Project{Requires: map[Dependency]Requirement{}}
	projectLock := NewProjectLock()
	projectLock.ToolchainVersion = "dev"

	result := dependencyWriteGolden{}
	appendChange := func(change filechange.Change, err error) {
		t.Helper()
		require.NoError(t, err)
		change.Path = filepath.Base(change.Path)
		result.Changes = append(result.Changes, change)
	}
	appendChange(WriteProjectChange(projectPath, project))
	appendChange(WriteProjectChange(projectPath, project))
	project.SetRequire(Dependency{URL: "example.com/library"}, "v1.0.0", false)
	appendChange(WriteProjectChange(projectPath, project))
	appendChange(WriteProjectLockChange(lockPath, projectLock))
	appendChange(WriteProjectLockChange(lockPath, projectLock))
	projectLock.ToolchainVersion = "v1.0.0"
	appendChange(WriteProjectLockChange(lockPath, projectLock))

	body, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	body = append(body, '\n')
	want, err := os.ReadFile("testdata/write-changes.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(body))
}
