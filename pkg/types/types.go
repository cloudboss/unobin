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

package types

type Predicate func() (bool, error)

type Action func() *Result

type TaskResult struct {
	TaskName string
	Result   *Result
}

type Result struct {
	Succeeded bool                   `json:"succeeded"`
	Changed   bool                   `json:"changed"`
	Rescued   bool                   `json:"rescued"`
	Error     string                 `json:"error,omitempty"`
	Module    string                 `json:"module"`
	Output    map[string]interface{} `json:"output,omitempty"`
}

type Context struct {
	Vars  map[string]interface{}
	State map[string]interface{}
	Item  interface{}
}

func DoIf(module string, condition Predicate, do Action) *Result {
	done, err := condition()
	if err != nil {
		return &Result{
			Succeeded: false,
			Changed:   false,
			Error:     err.Error(),
			Module:    module,
		}
	}
	if !done {
		return do()
	}
	return &Result{
		Succeeded: true,
		Changed:   false,
		Module:    module,
	}
}
