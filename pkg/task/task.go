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
	"fmt"

	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

type Block struct {
	Succeeded bool
	Body      []*Task
	Rescue    []*Task
	Always    []*Task
	When      func() (bool, error)
}

// We'll treat Block as a psuedo-module for cases when there is a failure at the
// block level outside of a task, for example if a "when" condition fails.
const blockModule = "block"

// Run executes the Body of a Block. On failure, it executes the Rescue
// clause if it is not nil. If the Rescue clause succeeds, then the Block
// proceeds to execute the Always clause if it is not nil. If the Rescue
// and Always clause succeed, then the Block succeeds.
func (b *Block) Run(results chan (*types.TaskResult), done chan (bool)) {
	b.Succeeded = true
	if b.When != nil {
		execute, err := b.When()
		if err != nil {
			b.Succeeded = false
			taskResult := util.TaskResultFailedUnchanged(blockModule, blockModule, err.Error())
			results <- taskResult
			if b.Rescue != nil {
				succeeded := runClause(b.Rescue, results)
				taskResult.Result.Rescued = succeeded
			}
			goto always
		}
		if !execute {
			results <- util.TaskResultSuceededUnchanged(blockModule, blockModule)
			done <- true
			return
		}
	}
	for _, task := range b.Body {
		var err error
		task.Module, err = unwrapModule(task)
		if err != nil {
			results <- util.TaskResultFailedUnchanged(task.Name, task.Module.Name(), err.Error())
			return
		}
		result := task.Module.Apply()
		results <- &types.TaskResult{TaskName: task.Name, Result: result}
		if !result.Succeeded {
			if b.Rescue != nil {
				succeeded := runClause(b.Rescue, results)
				result.Rescued = succeeded
			} else {
				b.Succeeded = false
			}
			break
		}
	}
always:
	fmt.Printf("It's always!\n")
	if b.Always != nil {
		succeeded := runClause(b.Always, results)
		if !succeeded {
			b.Succeeded = false
		}
	}
	done <- b.Succeeded
}

func unwrapModule(task *Task) (module.Module, error) {
	mod, err := task.Unwrap()
	if err != nil {
		return mod, err
	}
	err = mod.Initialize()
	if err != nil {
		return mod, err
	}
	return mod, nil
}

// runClause runs the Rescue or Always clause of a Block. It writes results to the
// channel and returns a bool indicating whether or not the entire clause succeeded.
func runClause(clause []*Task, results chan (*types.TaskResult)) bool {
	fmt.Printf("It's the run clause! %+v\n", clause)
	for i, task := range clause {
		fmt.Printf("i: %d\n", i)
		var err error
		task.Module, err = unwrapModule(task)
		if err != nil {
			results <- util.TaskResultFailedUnchanged(task.Name, task.Module.Name(), err.Error())
			return false
		}
		result := task.Module.Apply()
		results <- &types.TaskResult{TaskName: task.Name, Result: result}
		if !result.Succeeded {
			return false
		}
	}
	return true
}

type Task struct {
	Name       string
	ModuleName string
	Module     module.Module
	Unwrap     func() (module.Module, error)
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
