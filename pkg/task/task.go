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

package task

import (
	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

type Task struct {
	Description  string
	UnwrapModule func() (module.Module, error)
	When         func() (bool, error)
	Body         []*Task
	Rescue       []*Task
	Always       []*Task
	Context      *types.Context
	Succeeded    bool
	module       module.Module
}

func (t *Task) Run() []*types.Result {
	t.Succeeded = true
	if t.UnwrapModule != nil {
		// "Simple" task which has a non-nil UnwrapModule and a nil Body.
		return t.run(func() []*types.Result {
			results := []*types.Result{}
			result := t.module.Apply()
			util.PrintResult(result)
			results = append(results, result)
			if !result.Succeeded {
				if t.Rescue != nil {
					succeeded, rescueResults := runTasks(t.Rescue)
					t.Succeeded = succeeded
					results = append(results, rescueResults...)
				} else {
					t.Succeeded = false
				}
			} else {
				if result.Output != nil && t.Description != "" {
					t.Context.State[t.Description] = result.Output
				}
			}
			return results
		})
	} else {
		// "Compound" task which has a nil UnwrapModule and a non-nil Body.
		return t.run(func() []*types.Result {
			succeeded, results := runTasks(t.Body)
			if !succeeded {
				if t.Rescue != nil {
					succeeded, rescueResults := runTasks(t.Rescue)
					t.Succeeded = succeeded
					results = append(results, rescueResults...)
				} else {
					t.Succeeded = false
				}
			}
			return results
		})
	}
}

func (t *Task) run(runCore func() []*types.Result) []*types.Result {
	results := []*types.Result{}
	if t.UnwrapModule != nil {
		err := t.unwrapModule()
		if err != nil {
			// Unwrapping of a module always returns the module, even
			// on error, so it is safe to call t.module.Name().
			result := util.ResultFailedUnchanged(t.module.Name(), err.Error())
			util.PrintResult(result)
			results = append(results, result)
			if t.Rescue != nil {
				succeeded, rescueResults := runTasks(t.Rescue)
				t.Succeeded = succeeded
				results = append(results, rescueResults...)
			} else {
				t.Succeeded = false
			}
			if t.Always != nil {
				succeeded, alwaysResults := runTasks(t.Always)
				// Only modify t.Succeeded if the Always clause fails, as
				// the task might have already failed, and we don't want
				// a success here to set back it to true. But a failure
				// here should cause the whole task to fail.
				if !succeeded {
					t.Succeeded = false
				}
				results = append(results, alwaysResults...)
			}
			return results
		}
	}

	// A compound task has a pseudo-module name "_internal". This won't clash
	// with other module names as they are not allowed to contain underscores.
	moduleName := "_internal"
	if t.UnwrapModule != nil {
		moduleName = t.module.Name()
	}

	proceed, err := t.runWhen()
	if err != nil {
		t.Succeeded = false
		return []*types.Result{util.ResultFailedUnchanged(moduleName, err.Error())}
	}
	if !proceed {
		return []*types.Result{util.ResultSuceededUnchanged(moduleName)}
	}

	// Run the module of a simple task or the body of a compound task.
	results = append(results, runCore()...)

	if t.Always != nil {
		succeeded, alwaysResults := runTasks(t.Always)
		if !succeeded {
			t.Succeeded = false
		}
		results = append(results, alwaysResults...)
	}
	if len(results) > 0 {
		return results
	}
	return []*types.Result{util.ResultSuceededUnchanged(t.module.Name())}
}

func (t *Task) runWhen() (bool, error) {
	if t.When != nil {
		proceed, err := t.When()
		if err != nil {
			return false, err
		}
		return proceed, nil
	}
	return true, nil
}

func (t *Task) unwrapModule() error {
	var err error
	if t.module, err = t.UnwrapModule(); err != nil {
		return err
	}
	return t.module.Initialize()
}

func runTasks(tasks []*Task) (bool, []*types.Result) {
	results := []*types.Result{}
	for _, task := range tasks {
		results = append(results, task.Run()...)
		if !task.Succeeded {
			return false, results
		}
	}
	return true, results
}
