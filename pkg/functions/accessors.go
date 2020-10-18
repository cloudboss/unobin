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

package functions

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/playbook"
	"github.com/cloudboss/unobin/pkg/types"
)

func AnyVar(ctx *types.Context, path String) Interface {
	if path.Error != nil {
		return Interface{Error: path.Error}
	}

	i, err := playbook.ResolveAny(ctx.Vars, path.Value)
	if err != nil {
		return Interface{nil, err}
	}
	return Interface{i, nil}
}

func BoolVar(ctx *types.Context, path String) Bool {
	if path.Error != nil {
		return Bool{Error: path.Error}
	}

	b, err := playbook.ResolveBool(ctx.Vars, path.Value)
	if err != nil {
		return Bool{false, err}
	}
	return Bool{b, nil}
}

func StringVar(ctx *types.Context, path String) String {
	if path.Error != nil {
		return String{Error: path.Error}
	}

	s, err := playbook.ResolveString(ctx.Vars, path.Value)
	if err != nil {
		return String{"", err}
	}
	return String{s, nil}
}

func AnyOutput(ctx *types.Context, task, path String) Interface {
	if task.Error != nil {
		return Interface{Error: task.Error}
	}
	if path.Error != nil {
		return Interface{Error: path.Error}
	}

	moduleOutput, ok := ctx.State[task.Value].(map[string]interface{})
	if !ok {
		return Interface{nil, fmt.Errorf("task `%s` output not found", task.Value)}
	}
	i, err := playbook.ResolveAny(moduleOutput, path.Value)
	if err != nil {
		return Interface{nil, err}
	}
	return Interface{i, nil}
}

func BoolOutput(ctx *types.Context, task, path String) Bool {
	if task.Error != nil {
		return Bool{Error: task.Error}
	}
	if path.Error != nil {
		return Bool{Error: path.Error}
	}

	moduleOutput, ok := ctx.State[task.Value].(map[string]interface{})
	if !ok {
		return Bool{false, fmt.Errorf("task `%s` output not found", task.Value)}
	}
	b, err := playbook.ResolveBool(moduleOutput, path.Value)
	if err != nil {
		return Bool{false, err}
	}
	return Bool{b, nil}
}

func StringOutput(ctx *types.Context, task, path String) String {
	if task.Error != nil {
		return String{Error: task.Error}
	}
	if path.Error != nil {
		return String{Error: path.Error}
	}

	moduleOutput, ok := ctx.State[task.Value].(map[string]interface{})
	if !ok {
		return String{"", fmt.Errorf("task `%s` output not found", task.Value)}
	}
	s, err := playbook.ResolveString(moduleOutput, path.Value)
	if err != nil {
		return String{"", err}
	}
	return String{s, nil}
}
