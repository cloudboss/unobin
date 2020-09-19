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
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudboss/go-player/pkg/compiler"
	"github.com/spf13/cobra"
)

var (
	playbook   string
	output     string
	compileCmd = &cobra.Command{
		Use:   "compile",
		Short: "Compile a YAML playbook to a binary",
		RunE: func(cmd *cobra.Command, args []string) error {
			fset := token.NewFileSet()

			pb, modules, err := compiler.Load(playbook)
			if err != nil {
				return err
			}

			if output == "" {
				output, err = playbookName(playbook)
				if err != nil {
					return err
				}
			}

			goPath := fmt.Sprintf("%s.go", output)
			file, err := os.Create(goPath)
			if err != nil {
				return err
			}

			astFile := compiler.Compile(pb, modules)
			ast.SortImports(fset, astFile)
			format.Node(file, fset, astFile)

			if err = compileGo(output, goPath); err != nil {
				cmd.SilenceUsage = true
			}
			return err
		},
	}
)

func init() {
	RootCmd.AddCommand(compileCmd)
	compileCmd.Flags().StringVarP(&playbook, "playbook", "p",
		"", "Path to YAML playbook")
	compileCmd.Flags().StringVarP(&output, "output", "o",
		"", "Output filename, defaulting to the prefix of the YAML playbook filename.")
}

func playbookName(path string) (string, error) {
	parts := strings.Split(path, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid playbook name %s", path)
	}
	return parts[0], nil
}

func compileGo(name, goPath string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("go", "build", "-o", name, goPath)
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(stderr.String())
	}
	return err
}
