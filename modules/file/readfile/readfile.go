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

package readfile

import (
	"encoding/base64"
	"errors"
	"io/ioutil"
	"os"

	"github.com/cloudboss/unobin/pkg/file"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

const moduleName = "readfile"

type ReadFile struct {
	Src string
}

func (r *ReadFile) Initialize() error {
	if r.Src == "" {
		return errors.New("src is required")
	}
	return nil
}

func (r *ReadFile) Name() string {
	return moduleName
}

func (r *ReadFile) Apply() *types.Result {
	path := file.AbsolutePathAt(r.Src)
	f, err := os.Open(path)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	defer f.Close()
	bites, err := ioutil.ReadAll(f)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	return &types.Result{
		Succeeded: true,
		Module:    moduleName,
		Output: map[string]interface{}{
			"content": base64.StdEncoding.EncodeToString(bites),
		},
	}
}

func (t *ReadFile) Destroy() *types.Result {
	return nil
}
