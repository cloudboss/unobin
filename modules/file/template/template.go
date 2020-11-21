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

package template

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/cloudboss/unobin/pkg/file"
	"github.com/cloudboss/unobin/pkg/playbook"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

const moduleName = "template"

type Template struct {
	Src   string
	Dest  string
	Mode  string
	Owner string
	Group string
	Vars  map[string]interface{}
	mode  os.FileMode
}

func (t *Template) Initialize() error {
	if t.Src == "" {
		return errors.New("src is required")
	}
	if t.Dest == "" {
		return errors.New("dest is required")
	}
	if t.Mode == "" {
		t.mode = os.FileMode(0644)
	} else {
		mode, err := strconv.ParseUint(t.Mode, 8, 32)
		if err != nil {
			return errors.New("invalid file mode")
		}
		t.mode = os.FileMode(mode)
	}
	return nil
}

func (t *Template) Name() string {
	return moduleName
}

func (t *Template) Apply() *types.Result {
	path := fmt.Sprintf("%s/%s", os.Getenv(playbook.CacheDirectoryEnv), t.Src)
	f, err := os.Open(path)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	defer f.Close()
	templateBytes, err := ioutil.ReadAll(f)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	tpl, err := template.New("output").Parse(string(templateBytes))
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	var buf bytes.Buffer
	err = tpl.Execute(&buf, t.Vars)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	changed, err := file.WriteOnChange(t.Dest, &buf, t.mode)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	if changed {
		return util.ResultSuceededChanged(moduleName)
	} else {
		return util.ResultSuceededUnchanged(moduleName)
	}
}

func (t *Template) Destroy() *types.Result {
	return nil
}
