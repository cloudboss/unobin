package playbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/cloudboss/go-player/pkg/task"
	"github.com/cloudboss/go-player/pkg/types"
	"github.com/spf13/cobra"
	"github.com/xeipuuv/gojsonschema"
)

type PlaybookRepr struct {
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description"`
	Imports     map[string]string        `yaml:"imports"`
	InputSchema map[string]interface{}   `yaml:"input_schema"`
	Tasks       []map[string]interface{} `yaml:"tasks"`
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

func (p *Playbook) print(result *types.Result) error {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%v\n", string(b))
	return nil
}

func (p *Playbook) Run() []*types.Result {
	var results []*types.Result

	for _, task := range p.Tasks {
		err := task.Module.Initialize()
		if err != nil {
			result := &types.Result{
				Error:  err.Error(),
				Module: task.Module.Name(),
			}
			results = append(results, result)
			p.print(result)
			return results
		}
		result := task.Run()
		results = append(results, result)
		err = p.print(result)
		if err != nil {
			return results
		}
		if result.Error != "" {
			return results
		}
		if result.Output != nil {
			p.Context.State[task.Name] = result.Output
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

	var (
		varsFile     string
		applyCommand = &cobra.Command{
			Use:   "apply",
			Short: "Apply playbook",
			Run: func(cmd *cobra.Command, args []string) {
				vars, err := p.validateInputVars(varsFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", err)
					os.Exit(1)
				}

				p.Context.Vars = vars

				p.Run()
				if !p.Succeeded {
					os.Exit(1)
				}
			},
		}
	)

	root.AddCommand(applyCommand)
	applyCommand.Flags().StringVarP(&varsFile, "vars", "v", "", "File containing input variables")
	applyCommand.MarkFlagRequired("vars")

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
