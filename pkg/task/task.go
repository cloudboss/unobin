package task

import (
	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/types"
)

type Task struct {
	Name       string                 `json:"name"`
	ModuleName string                 `json:"module"`
	Params     map[string]interface{} `json:"params,omitempty"`
	Module     module.Module
	When       func() (bool, error)
}

func (t Task) Run() *types.Result {
	if t.When != nil {
		runTask, err := t.When()
		if err != nil {
			return &types.Result{
				Succeeded: false,
				Changed:   false,
				Module:    t.Module.Name(),
				Error:     err.Error(),
			}
		}
		if !runTask {
			return &types.Result{
				Succeeded: true,
				Changed:   false,
				Module:    t.Module.Name(),
			}
		}
	}
	return t.Module.Apply()
}
