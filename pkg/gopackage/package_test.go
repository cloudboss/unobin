package gopackage

import (
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadMatchesGoTargetFiles(t *testing.T) {
	for _, tt := range []struct {
		name string
		os   string
		arch string
		cgo  string
		tags string
		amd  string
		want []string
	}{
		{name: "linux", os: "linux", arch: "amd64", cgo: "0", amd: "v1",
			want: []string{"cpu_amd64.go", "feature_off.go", "library.go", "native_stub.go",
				"output_linux.go", "tuning_low.go"}},
		{name: "windows arm", os: "windows", arch: "arm64", cgo: "0",
			want: []string{"cpu_arm64.go", "feature_off.go", "library.go", "native_stub.go",
				"output_windows.go", "tuning_low.go"}},
		{name: "tags", os: "linux", arch: "amd64", cgo: "0", amd: "v1", tags: "-tags=special",
			want: []string{"cpu_amd64.go", "feature_on.go", "library.go", "native_stub.go",
				"output_linux.go", "tuning_low.go"}},
		{name: "cgo", os: "linux", arch: "amd64", cgo: "1", amd: "v1",
			want: []string{"cpu_amd64.go", "feature_off.go", "library.go", "native_cgo.go",
				"output_linux.go", "tuning_low.go"}},
		{name: "architecture feature", os: "linux", arch: "amd64", cgo: "0", amd: "v2",
			want: []string{"cpu_amd64.go", "feature_off.go", "library.go", "native_stub.go",
				"output_linux.go", "tuning_high.go"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOOS", tt.os)
			t.Setenv("GOARCH", tt.arch)
			t.Setenv("CGO_ENABLED", tt.cgo)
			t.Setenv("GOFLAGS", tt.tags)
			t.Setenv("GOAMD64", tt.amd)
			context, err := CurrentContext()
			require.NoError(t, err)
			pkg, err := Load("testdata/buildcontext", context)
			require.NoError(t, err)
			require.Equal(t, "buildcontext", pkg.Name)
			var active []string
			for _, file := range pkg.Files {
				if file.Active {
					active = append(active, file.Name)
				}
				require.NotEqual(t, "ignored_test.go", file.Name)
			}
			require.Equal(t, tt.want, active)
			cmd := exec.Command("go", "list", "-e", "-json", ".")
			cmd.Dir = "testdata/buildcontext"
			body, err := cmd.Output()
			require.NoError(t, err)
			var selected struct {
				GoFiles  []string
				CgoFiles []string
			}
			require.NoError(t, json.Unmarshal(body, &selected))
			goFiles := append(selected.GoFiles, selected.CgoFiles...)
			slices.Sort(goFiles)
			require.Equal(t, goFiles, active)
		})
	}
}

func TestArchitectureFeatureTags(t *testing.T) {
	for _, tt := range []struct {
		arch  string
		key   string
		value string
		want  []string
	}{
		{arch: "386", key: "GO386", value: "softfloat", want: []string{"386.softfloat"}},
		{arch: "arm", key: "GOARM", value: "6,hardfloat", want: []string{"arm.5", "arm.6"}},
		{arch: "arm64", key: "GOARM64", value: "v9.1,crypto", want: []string{
			"arm64.v9.0", "arm64.v9.1", "arm64.v8.0", "arm64.v8.1", "arm64.v8.2",
			"arm64.v8.3", "arm64.v8.4", "arm64.v8.5", "arm64.v8.6",
		}},
		{arch: "mipsle", key: "GOMIPS", value: "softfloat", want: []string{"mipsle.softfloat"}},
		{arch: "mips64", key: "GOMIPS64", value: "hardfloat", want: []string{"mips64.hardfloat"}},
		{arch: "ppc64le", key: "GOPPC64", value: "power10", want: []string{
			"ppc64le.power8", "ppc64le.power9", "ppc64le.power10",
		}},
		{arch: "riscv64", key: "GORISCV64", value: "rva23u64", want: []string{
			"riscv64.rva20u64", "riscv64.rva22u64", "riscv64.rva23u64",
		}},
		{arch: "wasm", key: "GOWASM", value: "signext", want: []string{"wasm.satconv", "wasm.signext"}},
	} {
		t.Run(tt.arch, func(t *testing.T) {
			t.Setenv("GOARCH", tt.arch)
			t.Setenv(tt.key, tt.value)
			context, err := CurrentContext()
			require.NoError(t, err)
			var tags []string
			for _, tag := range context.Build.ToolTags {
				if strings.HasPrefix(tag, tt.arch+".") {
					tags = append(tags, tag)
				}
			}
			require.Equal(t, tt.want, tags)
		})
	}
}

func TestCurrentContextReadsGoFlags(t *testing.T) {
	for _, tt := range []struct {
		flags string
		want  []string
		err   string
	}{
		{flags: "-tags=special,other", want: []string{"special", "other"}},
		{flags: "'-tags=special other'", want: []string{"special", "other"}},
		{flags: "-race -tags=special", want: []string{"special", "race"}},
		{flags: "-tags=special -race=false", want: []string{"special"}},
		{flags: "-tags", err: "tags requires a value"},
		{flags: "'-tags=special", err: "unterminated quote"},
	} {
		t.Run(tt.flags, func(t *testing.T) {
			t.Setenv("GOFLAGS", tt.flags)
			context, err := CurrentContext()
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, context.Build.BuildTags)
		})
	}
}

func TestContextDigestIncludesTarget(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("GOAMD64", "v1")
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "")
	before, err := CurrentContext()
	require.NoError(t, err)
	for key, value := range map[string]string{
		"GOOS": "windows", "GOARCH": "arm64", "CGO_ENABLED": "1",
		"GOFLAGS": "-tags=special", "GOAMD64": "v2", "GOEXPERIMENT": "none",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			after, err := CurrentContext()
			require.NoError(t, err)
			require.NotEqual(t, before.Digest(), after.Digest())
		})
	}
}

func TestLoadReportsSourceErrors(t *testing.T) {
	context, err := CurrentContext()
	require.NoError(t, err)
	_, err = Load(t.TempDir(), context)
	require.ErrorContains(t, err, "no Go package")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/first.go", []byte("package first\n"), 0o644))
	require.NoError(t, os.WriteFile(dir+"/second.go", []byte("package second\n"), 0o644))
	_, err = Load(dir, context)
	require.ErrorContains(t, err, "more than one Go package")
}
