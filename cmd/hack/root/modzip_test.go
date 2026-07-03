package root

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModZipCommandReportsMissingFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "all flags",
			want: "missing required flags: --module, --version, --repo, --out",
		},
		{
			name: "repo and out",
			args: []string{"--module", "example.com/m", "--version", "v0.1.0"},
			want: "missing required flags: --repo, --out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newModZipCmd()
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestModZipCommandCreatesValidModuleZip(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}

	repo := t.TempDir()
	writeTestFile(t, repo, "go.mod", "module example.com/m\n\ngo 1.26\n")
	writeTestFile(t, repo, "cmd/tool/main.go", "package main\n\nfunc main() {}\n")
	writeTestFile(t, repo, "testdata/nested/go.mod", "module example.com/nested\n")
	writeTestFile(t, repo, "testdata/nested/nested.go", "package nested\n")

	runGit(t, git, repo, "init")
	runGit(t, git, repo, "config", "user.email", "test@example.com")
	runGit(t, git, repo, "config", "user.name", "Test User")
	runGit(t, git, repo, "add", ".")
	runGit(t, git, repo, "commit", "-m", "initial")
	runGit(t, git, repo, "tag", "v0.1.0")

	out := filepath.Join(t.TempDir(), "v0.1.0.zip")
	cmd := newModZipCmd()
	cmd.SetArgs([]string{
		"--module", "example.com/m",
		"--version", "v0.1.0",
		"--repo", repo,
		"--out", out,
	})
	require.NoError(t, cmd.Execute())

	zr, err := zip.OpenReader(out)
	require.NoError(t, err)
	defer zr.Close()

	names := make([]string, 0, len(zr.File))
	for _, file := range zr.File {
		names = append(names, file.Name)
	}
	require.Contains(t, names, "example.com/m@v0.1.0/go.mod")
	require.Contains(t, names, "example.com/m@v0.1.0/cmd/tool/main.go")
	require.NotContains(t, names, "example.com/m@v0.1.0/testdata/nested/go.mod")
	require.NotContains(t, names, "example.com/m@v0.1.0/testdata/nested/nested.go")
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func runGit(t *testing.T, git, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}
