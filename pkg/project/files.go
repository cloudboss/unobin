package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/projectmarker"
)

func projectRoot(stackPath string) (string, error) {
	root, marker, err := deps.FindProjectMarkerDir(stackPath)
	if err == nil {
		if marker.Kind == projectmarker.Go {
			return "", fmt.Errorf("deps sync manages UB projects; use Go commands for Go modules")
		}
		return root, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if info, err := os.Stat(stackPath); err == nil && info.IsDir() {
		return stackPath, nil
	}
	return filepath.Dir(stackPath), nil
}

func readProjectOrEmpty(root string) (*deps.Project, string, error) {
	project, err := deps.ReadProject(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		return &deps.Project{
			Requires: map[deps.Dependency]deps.Requirement{},
		}, deps.ProjectFileName, nil
	}
	if err != nil {
		return nil, deps.ProjectFileName, err
	}
	return project, deps.ProjectFileName, nil
}

func writeDependencyFiles(
	root string,
	project *deps.Project,
	projectLock *deps.ProjectLock,
) (*DependencyWriteResult, error) {
	result := &DependencyWriteResult{
		ProjectFile: deps.ProjectFileName,
		LockFile:    deps.ProjectLockFileName,
		Direct:      project.DirectCount(),
		Indirect:    project.IndirectCount(),
		Selected:    len(projectLock.Deps),
		Files:       []filechange.Change{},
	}
	projectPath := filepath.Join(root, deps.ProjectFileName)
	projectChange, err := deps.WriteProjectChange(projectPath, project)
	if err != nil {
		return nil, err
	}
	projectChange.Path = deps.ProjectFileName
	result.Files = append(result.Files, projectChange)
	lockPath := filepath.Join(root, deps.ProjectLockFileName)
	lockChange, err := deps.WriteProjectLockChange(lockPath, projectLock)
	if err != nil {
		return result, err
	}
	lockChange.Path = deps.ProjectLockFileName
	result.Files = append(result.Files, lockChange)
	result.Files, err = filechange.Compose(result.Files)
	if err != nil {
		return result, err
	}
	return result, nil
}

func readProjectLock(stackPath string) (*deps.ProjectLock, error) {
	root, rootErr := projectRoot(stackPath)
	if rootErr != nil {
		return nil, rootErr
	}
	projectLock, err := deps.ReadProjectLock(os.DirFS(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s found; run `unobin deps sync` first",
				deps.ProjectLockFileName)
		}
		return nil, err
	}
	return projectLock, nil
}

func readProjectLockOrNil(root string) (*deps.ProjectLock, error) {
	projectLock, err := deps.ReadProjectLock(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return projectLock, nil
}
