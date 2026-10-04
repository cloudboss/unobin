package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/filechange"
)

type dependencyPartialGolden struct {
	ProjectFile string              `json:"project-file"`
	LockFile    string              `json:"lock-file"`
	Direct      int                 `json:"direct"`
	Indirect    int                 `json:"indirect"`
	Selected    int                 `json:"selected"`
	Files       []filechange.Change `json:"files"`
	Error       string              `json:"error"`
}

func TestDependencyWritePartialGolden(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, deps.ProjectLockFileName)
	require.NoError(t, os.Mkdir(lockPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(lockPath, "keep"), []byte("x"), 0o644))
	project := &deps.Project{Requires: map[deps.Dependency]deps.Requirement{}}
	projectLock := deps.NewProjectLock()
	projectLock.ToolchainVersion = "dev"

	result, writeErr := writeDependencyFiles(dir, project, projectLock)
	require.Error(t, writeErr)
	require.NotNil(t, result)
	view := dependencyPartialGolden{
		ProjectFile: result.ProjectFile,
		LockFile:    result.LockFile,
		Direct:      result.Direct,
		Indirect:    result.Indirect,
		Selected:    result.Selected,
		Files:       result.Files,
		Error:       strings.ReplaceAll(writeErr.Error(), dir, "$TMP"),
	}
	body, err := json.MarshalIndent(view, "", "  ")
	require.NoError(t, err)
	body = append(body, '\n')
	want, err := os.ReadFile("testdata/dependency-write-partial.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(body))
}
