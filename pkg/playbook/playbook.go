package playbook

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/cloudboss/go-player/pkg/task"
	"github.com/cloudboss/go-player/pkg/types"
	"github.com/spf13/cobra"
)

type Playbook struct {
	Name        string
	Description string
	Tasks       []*task.Task
	Frame       *types.Frame
	Succeeded   bool
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
			p.Frame.State[task.Name] = result.Output
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

	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply playbook",
		Run: func(cmd *cobra.Command, args []string) {
			p.Run()
			if !p.Succeeded {
				os.Exit(1)
			}
		},
	}

	root.AddCommand(apply)
	root.Execute()
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
