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
)

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
