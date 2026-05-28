package root

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
)

var (
	fmtCfg = &fmtConfig{}
	FmtCmd = &cobra.Command{
		Use:   "fmt [paths...]",
		Short: "Format .ub source files",
		Long: `Reformat one or more .ub files in canonical form.

With no path arguments, fmt reads from stdin and writes the
formatted bytes to stdout. With one or more paths, each file is
formatted in turn; directory arguments are walked recursively for
*.ub files.

Flags:
  -w / --write   overwrite each file in place instead of writing to
                 stdout.
  -l / --list    print the names of files whose formatted output
                 differs from their current contents; no other output.

Examples:
  unobin fmt factory.ub
  unobin fmt -w factory.ub libraries/
  unobin fmt -l .
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFmt(cmd, args, fmtCfg)
		},
	}
)

type fmtConfig struct {
	write         bool
	list          bool
	maxLineLength int
	wrapStrings   bool
}

func init() {
	FmtCmd.Flags().BoolVarP(&fmtCfg.write, "write", "w", false,
		"Write the formatted output back to the source file.")
	FmtCmd.Flags().BoolVarP(&fmtCfg.list, "list", "l", false,
		"Print paths whose formatted output differs from their contents.")
	FmtCmd.Flags().IntVar(&fmtCfg.maxLineLength, "max-line-length", lang.DefaultMaxColumn,
		"Target line width for formatted output.")
	FmtCmd.Flags().BoolVar(&fmtCfg.wrapStrings, "wrap-strings", false,
		"Rewrite overflowing single-quoted strings as folded or joined triple-quoted form.")
}

func runFmt(cmd *cobra.Command, args []string, cfg *fmtConfig) error {
	if len(args) == 0 {
		return formatStdin(cmd.OutOrStdout(), cmd.InOrStdin(), cfg)
	}
	paths, err := expandFmtPaths(args)
	if err != nil {
		return err
	}
	changed := false
	for _, path := range paths {
		ok, err := formatPath(cmd.OutOrStdout(), path, cfg)
		if err != nil {
			return err
		}
		if !ok {
			changed = true
		}
	}
	if cfg.list && changed {
		// Listing returns 0 even when some files would change; gofmt
		// matches this. CI scripts test for non-empty output, not exit
		// status.
		_ = changed
	}
	return nil
}

func formatStdin(out io.Writer, in io.Reader, cfg *fmtConfig) error {
	src, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	formatted, err := formatBytes("<stdin>", src, cfg)
	if err != nil {
		return err
	}
	if cfg.list && !bytes.Equal(src, formatted) {
		fmt.Fprintln(out, "<stdin>")
		return nil
	}
	if cfg.list {
		return nil
	}
	_, err = out.Write(formatted)
	return err
}

// formatPath formats one file. Returns (unchanged, error): unchanged
// is true when the file's contents already match the canonical
// output. The boolean lets the caller summarize across many files.
func formatPath(out io.Writer, path string, cfg *fmtConfig) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	formatted, err := formatBytes(path, src, cfg)
	if err != nil {
		return false, err
	}
	unchanged := bytes.Equal(src, formatted)
	switch {
	case cfg.list:
		if !unchanged {
			fmt.Fprintln(out, path)
		}
	case cfg.write:
		if unchanged {
			return unchanged, nil
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return unchanged, fmt.Errorf("write %s: %w", path, err)
		}
	default:
		if _, err := out.Write(formatted); err != nil {
			return unchanged, err
		}
	}
	return unchanged, nil
}

func formatBytes(name string, src []byte, cfg *fmtConfig) ([]byte, error) {
	file, err := lang.ParseSource(name, src)
	if err != nil {
		return nil, err
	}
	return lang.FormatWith(file, lang.FormatOptions{
		MaxColumn:   cfg.maxLineLength,
		WrapStrings: cfg.wrapStrings,
	})
}

// expandFmtPaths walks each argument and returns the list of files to
// format. A directory expands to its descendant *.ub files; a file
// argument is returned as-is regardless of its extension so the
// operator can format a specifically-named file.
func expandFmtPaths(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", arg, err)
		}
		if !info.IsDir() {
			out = append(out, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".ub") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", arg, err)
		}
	}
	return out, nil
}
