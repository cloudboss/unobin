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

package copy

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/cloudboss/unobin/pkg/file"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

const moduleName = "copy"

type Copy struct {
	Src     string
	Dest    string
	Content string
	Mode    string
	Owner   string
	Group   string
	Vars    map[string]interface{}
	mode    os.FileMode
}

func (c *Copy) Initialize() error {
	if c.Src == "" && c.Content == "" {
		return errors.New("one of src or content is required")
	}
	if c.Dest == "" {
		return errors.New("dest is required")
	}
	if c.Mode == "" {
		c.mode = os.FileMode(0644)
	} else {
		mode, err := strconv.ParseUint(c.Mode, 8, 32)
		if err != nil {
			return errors.New("invalid file mode")
		}
		c.mode = os.FileMode(mode)
	}
	return nil
}

func (c *Copy) Name() string {
	return moduleName
}

func (c *Copy) Apply() *types.Result {
	var bites []byte
	if c.Src != "" {
		path := file.AbsolutePathAt(c.Src)
		f, err := os.Open(path)
		if err != nil {
			return util.ResultFailedUnchanged(moduleName, err.Error())
		}
		defer f.Close()
		bites, err = ioutil.ReadAll(f)
		if err != nil {
			return util.ResultFailedUnchanged(moduleName, err.Error())
		}
	} else {
		bites = []byte(c.Content)
	}
	buf := bytes.NewBuffer(bites)
	changed, err := file.WriteOnChange(c.Dest, buf, c.mode)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}
	if changed {
		return util.ResultSuceededChanged(moduleName)
	} else {
		return util.ResultSuceededUnchanged(moduleName)
	}
}

func (c *Copy) Destroy() *types.Result {
	return nil
}
