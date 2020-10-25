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
