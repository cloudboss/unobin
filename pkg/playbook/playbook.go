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

package playbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/task"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/spf13/cobra"
	"github.com/xeipuuv/gojsonschema"
)

const CacheDirectoryEnv = "UNOBIN_CACHE_DIRECTORY"

type PlaybookAttributes struct {
	Name        string
	Description string
	Imports     map[string]string
	InputSchema map[string]interface{}
}

type Playbook struct {
	Name        string
	Description string
	Imports     map[string]string
	InputSchema map[string]interface{}
	Tasks       []*task.Task
	Context     *types.Context
	Succeeded   bool
	Outputs     map[string]interface{}
	Resources   []Resource
}

type Resource struct {
	Path     string
	Info     os.FileInfo
	Contents []byte
}

func NewPlaybook(playbookPath, moduleSearchPath string) (*Playbook, error) {
	jsn, err := ioutil.ReadFile(playbookPath)
	if err != nil {
		return nil, err
	}
	var playbook Playbook
	err = json.Unmarshal(jsn, &playbook)
	if err != nil {
		return nil, err
	}

	return &playbook, nil
}

func (p *Playbook) Apply() []*types.Result {
	var results []*types.Result

	for _, task := range p.Tasks {
		results = append(results, task.Run()...)
		if !task.Succeeded {
			return results
		}
	}

	p.Succeeded = true
	return results
}

func (p *Playbook) StartCLI() {
	root := &cobra.Command{
		Use:   p.Name,
		Short: p.Description,
	}

	defaultCacheDirectory := fmt.Sprintf("~/.unobin/%s", p.Name)

	var (
		varsFile       string
		cacheDirectory string
		applyCommand   = &cobra.Command{
			Use:   "apply",
			Short: fmt.Sprintf("Apply %s", p.Name),
			PreRunE: func(cmd *cobra.Command, args []string) error {
				if cacheDirectory == defaultCacheDirectory {
					currentUser, err := user.Current()
					if err != nil {
						return err
					}
					cacheDirectory = fmt.Sprintf("%s/.unobin/%s", currentUser.HomeDir, p.Name)
				}
				return nil
			},
			Run: func(cmd *cobra.Command, args []string) {
				err := p.cacheResources(cacheDirectory)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", err)
					os.Exit(1)
				}

				// Set environment variable so modules know where to look for cached files.
				err = os.Setenv(CacheDirectoryEnv, cacheDirectory)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", err)
					os.Exit(1)
				}

				vars, err := p.validateInputVars(varsFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", err)
					os.Exit(1)
				}

				p.Context.Vars = vars

				p.Apply()
				if !p.Succeeded {
					os.Exit(1)
				}
			},
		}
	)

	root.AddCommand(applyCommand)
	applyCommand.Flags().StringVarP(&varsFile, "vars", "v", "", "File containing input variables")
	applyCommand.MarkFlagRequired("vars")
	applyCommand.Flags().StringVarP(&cacheDirectory, "cache-directory", "c", defaultCacheDirectory,
		"Directory where resources will be cached")

	root.Execute()
}

func (p *Playbook) validateInputVars(varsFile string) (map[string]interface{}, error) {
	if _, err := os.Stat(varsFile); os.IsNotExist(err) {
		return nil, errors.New(fmt.Sprintf("file %s not found", varsFile))
	}

	jsn, err := ioutil.ReadFile(varsFile)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("error reading file %s: %v", varsFile, err))
	}

	inputSchemaLoader := gojsonschema.NewGoLoader(p.InputSchema)
	document := gojsonschema.NewStringLoader(string(jsn))

	result, err := gojsonschema.Validate(inputSchemaLoader, document)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("failed to validate file %s: %v", varsFile, err))
	}
	if !result.Valid() {
		errMsg := fmt.Sprintf("file %s is not valid: ", varsFile)
		for i, err := range result.Errors() {
			var sep string
			if i == 0 {
				sep = ""
			} else {
				sep = "; "
			}
			errMsg += fmt.Sprintf("%s%s", sep, err)
		}
		return nil, errors.New(errMsg)
	}

	// Unmarshal error is ignored because JSON is already validated.
	var vars map[string]interface{}
	json.Unmarshal(jsn, &vars)

	return vars, nil
}

func (p *Playbook) cacheResources(cacheDirectory string) error {
	for _, resource := range p.Resources {
		fullPath := fmt.Sprintf("%s/%s", cacheDirectory, resource.Path)
		dir := filepath.Dir(fullPath)
		err := os.MkdirAll(dir, os.FileMode(0755))
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(fullPath, resource.Contents, resource.Info.Mode())
		if err != nil {
			return err
		}
		err = os.Chtimes(fullPath, resource.Info.ModTime(), resource.Info.ModTime())
		if err != nil {
			return err
		}
	}
	return nil
}

func ResolveMap(attributes map[string]interface{}, path string) (map[string]interface{}, error) {
	parts := strings.Split(path, ".")
	innerAttributes := attributes
	for _, part := range parts {
		var ok bool
		if innerAttributes, ok = innerAttributes[part].(map[string]interface{}); !ok {
			return nil, fmt.Errorf("map attribute `%s` not found", path)
		}
	}
	return innerAttributes, nil
}

func ResolveString(attributes map[string]interface{}, path string) (string, error) {
	parts := strings.Split(path, ".")
	innerAttributes := attributes
	for _, part := range parts[:len(parts)-1] {
		var ok bool
		if innerAttributes, ok = innerAttributes[part].(map[string]interface{}); !ok {
			return "", fmt.Errorf("string attribute `%s` not found", path)
		}
	}
	if s, ok := innerAttributes[parts[len(parts)-1]].(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("string attribute `%s` not found", path)
}

func ResolveBool(attributes map[string]interface{}, path string) (bool, error) {
	parts := strings.Split(path, ".")
	innerAttributes := attributes
	for _, part := range parts[:len(parts)-1] {
		var ok bool
		if innerAttributes, ok = innerAttributes[part].(map[string]interface{}); !ok {
			return false, fmt.Errorf("bool attribute `%s` not found", path)
		}
	}
	if b, ok := innerAttributes[parts[len(parts)-1]].(bool); ok {
		return b, nil
	}
	return false, fmt.Errorf("bool attribute `%s` not found", path)
}

func ResolveAny(attributes map[string]interface{}, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	innerAttributes := attributes
	for _, part := range parts[:len(parts)-1] {
		var ok bool
		if innerAttributes, ok = innerAttributes[part].(map[string]interface{}); !ok {
			return false, fmt.Errorf("interface{} attribute `%s` not found", path)
		}
	}
	if b, ok := innerAttributes[parts[len(parts)-1]]; ok {
		return b, nil
	}
	return false, fmt.Errorf("interface{} attribute `%s` not found", path)
}
