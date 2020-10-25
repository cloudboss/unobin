// Copyright © 2020 Joseph Wright <joseph@cloudboss.co>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
)

var (
	importPath  string
	projectPath string
	initCmd     = &cobra.Command{
		Use:   "init",
		Short: "Initialize a unobin project",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if importPath == "" {
				err = multierror.Append(err, fmt.Errorf("--import-path is required"))
			}
			if projectPath == "" {
				err = multierror.Append(err, fmt.Errorf("--project-path is required"))
			}
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.MkdirAll(projectPath, 0755); err != nil {
				return err
			}
			for _, path := range AssetNames() {
				err := writeToProject(path)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
)

func init() {
	RootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVarP(&importPath, "import-path", "i",
		"", "Go import path of the project")
	initCmd.Flags().StringVarP(&projectPath, "project-path", "p",
		"", "Filesystem path for the project")
}

func writeToProject(path string) error {
	projectFile := fmt.Sprintf("%s/%s", projectPath, path)
	projectDir := filepath.Dir(projectFile)

	// Ensure destination directory is present.
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return err
	}
	info, err := AssetInfo(path)
	if err != nil {
		return err
	}
	outFile, err := os.OpenFile(projectFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	contents, err := Asset(path)
	if err != nil {
		return err
	}
	t, err := template.New(projectFile).Parse(string(contents))
	if err != nil {
		return err
	}
	return t.Execute(outFile, map[string]string{
		"ImportPath": importPath,
		"Project":    projectPath,
	})
}
