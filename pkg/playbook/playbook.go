package playbook

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/cloudboss/go-player/pkg/task"
	"github.com/cloudboss/go-player/pkg/types"
)

type Playbook struct {
	Vars      map[string]interface{}
	State     map[string]interface{}
	Tasks     []*task.Task
	Succeeded bool
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

	p.State = map[string]interface{}{}

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
			p.State[task.Name] = result.Output
		}
	}

	p.Succeeded = true
	return results
}

func resolveMap(attributes map[string]interface{}, path string) map[string]interface{} {
	parts := strings.Split(path, ".")
	innerAttributes := attributes
	for _, part := range parts {
		var ok bool
		if innerAttributes, ok = innerAttributes[part].(map[string]interface{}); !ok {
			return nil
		}
	}
	return innerAttributes
}

func resolveString(attributes map[string]interface{}, path string) string {
	parts := strings.Split(path, ".")
	innerAttributes := attributes
	for _, part := range parts[:len(parts)-1] {
		var ok bool
		if innerAttributes, ok = innerAttributes[part].(map[string]interface{}); !ok {
			return ""
		}
	}
	if s, ok := innerAttributes[parts[len(parts)-1]].(string); ok {
		return s
	}
	return ""
}
