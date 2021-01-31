// Copyright © 2021 Joseph Wright <joseph@cloudboss.co>
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

package filesystem

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloudboss/unobin/pkg/commands"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

const (
	moduleName = "filesystem"
)

type Filesystem struct {
	// Device is the path to the device on which to create the filesystem.
	Device string
	// Attempt to force filesystem to be created. This can still fail, for
	// example if a filesystem is present on the device and it is mounted.
	Force bool
	// Fstype is the type of the filesystem.
	Fstype string
	// Opts is an array of options to pass when creating the filesystem.
	Opts []interface{}
	// Values from Opts are converted to strings
	// and copied to opts.
	opts []string
}

func (f *Filesystem) Initialize() error {
	if f.Device == "" {
		return errors.New("device is required")
	}

	fsChoices := []string{"ext2", "ext3", "ext4", "vfat", "xfs"}
	err := util.IsOneOfString(f.Fstype, "fstype", fsChoices, true)
	if err != nil {
		return err
	}
	f.Fstype = strings.ToLower(f.Fstype)

	if f.Opts == nil {
		f.opts = []string{}
	} else {
		for _, opt := range f.Opts {
			if value, ok := opt.(string); !ok {
				return errors.New("opts must be strings")
			} else {
				f.opts = append(f.opts, value)
			}
		}
	}

	return nil
}

func (f *Filesystem) Name() string {
	return moduleName
}

func (f *Filesystem) Apply() *types.Result {
	blkid, err := commands.RunCommand("blkid", "-c", "/dev/null", "-o", "value", "-s", "TYPE", f.Device)
	if err != nil {
		errMsg := fmt.Sprintf("failed check for existing filesystem: %v", err)
		result := util.ResultFailedUnchanged(moduleName, errMsg)
		result.Output = output(blkid, f.Device, f.Fstype)
		return result
	}
	if blkid.Stdout == f.Fstype && !f.Force {
		result := util.ResultSuceededUnchanged(moduleName)
		result.Output = output(nil, f.Device, f.Fstype)
		return result
	}
	fsf, err := newFilesystemFormatter(f.Fstype, f.Device, f.opts)
	if err != nil {
		result := util.ResultFailedUnchanged(moduleName, err.Error())
		result.Output = output(nil, f.Device, f.Fstype)
		return result
	}
	var result *types.Result
	err = fsf.create()
	if err != nil {
		result = util.ResultFailedChanged(moduleName, err.Error())
	} else {
		result = util.ResultSuceededChanged(moduleName)
	}
	result.Output = fsf.output()
	return result
}

func (f *Filesystem) Destroy() *types.Result {
	return nil
}

type filesystemFormatter struct {
	device        string
	fstype        string
	mkfs          string
	opts          []string
	commandOutput *commands.CommandOutput
}

func newFilesystemFormatter(fstype, device string, opts []string) (*filesystemFormatter, error) {
	mkfs := fmt.Sprintf("mkfs.%s", fstype)
	mkfsPath, err := exec.LookPath(mkfs)
	if err != nil {
		return nil, fmt.Errorf("%s not found", mkfs)
	}
	switch fstype {
	case "ext2":
	case "ext3":
	case "ext4":
		opts = append(opts, "-F")
	case "xfs":
		opts = append(opts, "-f")
	}
	opts = append(opts, device)
	return &filesystemFormatter{mkfs: mkfsPath, fstype: fstype, device: device, opts: opts}, nil
}

func (f *filesystemFormatter) create() error {
	var err error
	f.commandOutput, err = commands.RunCommand(f.mkfs, f.opts...)
	if err != nil {
		return fmt.Errorf("failed to create filesystem on device %s: %v", f.device, err)
	}
	if f.commandOutput.ExitStatus != 0 {
		if f.commandOutput.Stderr != "" {
			return fmt.Errorf("failed to create filesystem on device %s: %s",
				f.device, f.commandOutput.Stderr)
		}
		return fmt.Errorf("unknown error creating filesystem on device %s", f.device)
	}
	return nil
}

func (f *filesystemFormatter) output() map[string]interface{} {
	return output(f.commandOutput, f.device, f.fstype)
}

func output(cmd *commands.CommandOutput, device, fstype string) map[string]interface{} {
	out := map[string]interface{}{
		"device": device,
		"fstype": fstype,
	}
	if cmd != nil {
		out["stdout"] = cmd.Stdout
		out["stderr"] = cmd.Stderr
		out["stdout_lines"] = cmd.StdoutLines
		out["stderr_lines"] = cmd.StderrLines
	} else {
		out["stdout"] = ""
		out["stderr"] = ""
		out["stdout_lines"] = []string{}
		out["stderr_lines"] = []string{}
	}
	return out
}
