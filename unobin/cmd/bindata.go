// Code generated for package cmd by go-bindata DO NOT EDIT. (@generated)
// sources:
// unobin/cmd/templates/go.mod
// unobin/cmd/templates/playbook.ub
// unobin/cmd/templates/resources/hello.txt.tmpl
package cmd

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _goMod = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xca\xcd\x4f\x29\xcd\x49\x55\xa8\xae\x56\xd0\xf3\xcc\x2d\xc8\x2f\x2a\x09\x48\x2c\xc9\x50\xa8\xad\xe5\x02\x04\x00\x00\xff\xff\xc8\x9d\xfe\x34\x19\x00\x00\x00")

func goModBytes() ([]byte, error) {
	return bindataRead(
		_goMod,
		"go.mod",
	)
}

func goMod() (*asset, error) {
	bytes, err := goModBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "go.mod", size: 25, mode: os.FileMode(420), modTime: time.Unix(1600666172, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _playbookUb = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x8c\x55\x4d\x6f\xdc\x36\x10\xbd\xeb\x57\x4c\x60\x04\xb4\x01\x5b\x9b\x43\xd0\xc3\xde\x8a\x1e\x9a\xe6\xd0\x04\x68\x6e\x86\x51\x8c\xa8\x59\x8b\x5d\x8a\x54\x38\xc3\xfd\x80\xb1\xff\xbd\x18\x52\x5a\xef\x3a\x2d\x90\x83\x61\x4b\x7e\x7c\xf3\xe6\xcd\xe3\x28\xe0\x48\x6b\x30\x2f\x2f\xd0\x7e\x4d\xf1\x1f\xb2\x02\xa7\x93\x69\x7a\x62\x9b\xdc\x24\x2e\x86\x35\x98\x5f\xa1\x73\x01\xd3\x11\x26\x8f\xc7\x2e\xc6\xad\x69\x9a\x1b\xf8\x63\x9c\x62\x12\x06\x4c\x04\x63\xec\xb3\x27\x86\x7d\x72\x22\x14\xc0\x05\xf8\x3d\xb6\xf0\x6d\x20\x70\x05\x06\x5b\x3a\x32\x8c\x28\x76\x00\x19\x5e\x0f\xb8\xd0\xdc\x80\x20\x6f\xb9\xa2\x27\x94\x01\x24\x16\xcc\x7c\xd2\x71\x79\x62\x1c\x09\x90\x2b\xf5\x3d\x74\x59\x60\xef\xe4\x92\x4d\x99\x8e\x13\x01\x4e\x13\x85\x9e\x7a\x45\x23\x70\xde\x6c\xdc\xa1\x6d\x2a\x1b\xaf\xe1\xa5\x01\xb0\x63\xbf\x06\xf3\xec\x64\xc8\x5d\x6b\xe3\xb8\xb2\x3e\xe6\xbe\x8b\xcc\xab\x1c\x62\xe7\xc2\x6a\x16\xb8\xb2\x71\x1c\x31\xf4\xed\x6f\xf5\xb7\x69\x00\x84\xc6\xc9\xa3\xd0\x4f\x32\x6c\x9c\xa7\xd5\x72\xa6\xfd\x36\xff\x61\x9a\x93\x9a\x58\x1c\x0a\x53\x16\x60\x3b\xd0\x88\xda\x2d\xc2\xe7\xbf\xbe\xfc\xb9\xbc\xd8\xc4\x04\x3b\xf4\xae\x47\x71\xe1\x79\x06\xef\x30\x39\xec\x3c\x2d\xae\xcd\x73\x69\x6e\x60\xef\xbc\x87\x10\x05\xd0\x5a\x9a\xe4\x2d\x1e\x64\x40\x81\x3e\x16\xc8\x4c\x4b\x6d\x53\x50\x0f\xb5\x62\x75\x48\x9d\x5c\x83\x89\x9d\x66\x42\xbb\x9e\x52\x9c\x28\x89\xa3\xd9\x42\x80\x9a\x9d\x97\x05\xca\x92\x5c\x78\x36\x70\x6a\xa0\xfc\x24\xfa\x9e\x5d\xa2\x7e\x0d\x8f\x46\xa1\xe6\xa9\x01\xc0\xbe\x77\x9a\x2a\xf4\x5f\x2f\xf8\x36\xe8\x99\xd4\x90\xce\x47\xbb\x2d\xf4\x37\xb0\x1f\x28\x3c\xd0\x81\x6c\x16\xba\xbd\x83\x94\x03\x83\x2b\x79\x7b\xce\x23\x05\x01\x0c\x3d\x70\xb6\x96\xa8\x67\x70\x9b\x92\x04\x3a\x38\x01\x16\x94\xcc\x40\xdf\x33\x7a\x86\x0f\x6d\x03\x85\x6c\x7d\x4d\x69\x56\x3a\x25\x49\x99\xcc\x5d\x53\x2a\xaa\x95\xaa\x14\xe2\xe6\x22\x56\xf7\x9a\x96\x7b\x18\x33\xcb\x1c\xe0\x18\xce\x90\x8b\x74\xb7\x35\x57\xf0\x38\x90\xf7\x11\x36\x29\x8e\x80\xe1\xcd\x00\xde\x3d\xcd\xe6\xd5\x6a\x9b\x98\x46\x14\xd8\xe4\x60\xd5\x15\xe8\x68\xc0\x1d\x31\x78\xb7\x25\xe0\x29\xb9\x20\x9b\xb6\xe0\x67\xd5\xeb\xf9\xc8\xad\x21\x3b\x44\xf8\x54\x4a\xbd\xe7\x77\x06\x30\x1c\x1f\x76\x98\x6e\xab\xd7\x77\x77\x65\x0a\xa7\xa6\x29\x92\x0a\x58\xf5\x4a\xca\x32\x3c\xcd\x0e\xab\x82\x98\x45\xf5\x95\x76\x1c\xc3\x1c\xf7\x1a\xa3\x8e\x20\x73\xbd\x47\x72\xce\xa9\xc4\x62\xe3\xdf\xb3\xa0\x42\xe4\x42\x01\x04\x3a\x48\xb9\xcc\x2a\xf9\x2c\xb8\x2a\x7d\x35\xfb\xed\x9c\x3f\x51\xa2\xfb\xb7\xd3\xce\x4c\xb5\xa8\xb9\x96\x6e\x0a\xff\xac\xda\x70\x21\x30\x2c\x7d\xcc\x62\x00\x45\x92\xeb\xb2\x50\xbd\xf9\x4b\x2f\x12\x35\x3c\xff\x93\x82\x9a\xda\x87\x4a\x78\xfb\x43\xb5\x85\xfb\xae\x44\xa4\x78\x99\x72\x00\x7f\x76\x6a\x99\xe7\x6b\xbb\x9e\x61\x65\xae\xdc\xbf\x3e\xa1\x51\xd5\x9d\x00\x7d\x24\x0e\x46\x34\xb3\x2c\x95\xe7\x47\x16\x9d\x92\xe3\xf9\x5a\xd3\x8e\x92\xf6\x02\xec\x82\x25\x58\x01\xfa\x3d\x1e\xb9\x32\xd4\x00\x26\x42\xd1\x2b\x65\x56\xe6\xaa\xfe\xab\x1d\x99\x75\x8f\x60\x88\x32\x50\x5a\xde\x1a\x9e\x3d\x7d\xa3\xe3\x2a\x6d\x5f\xce\x59\xb9\xec\x86\xd7\xf0\x9e\x6b\xfe\x16\x17\xaf\x1b\xbe\x72\xf1\xd4\x34\xcb\x2e\x84\x47\x3a\x4c\x4a\x81\xe7\x95\xba\x24\x93\x93\xd5\x4d\x98\xc8\xa3\xb8\x1d\x2d\x5f\x84\xf3\xf7\x47\xff\xc5\x31\x27\x4b\x0c\xbd\x4b\x64\x25\xa6\xa3\xf6\xcf\xc9\xae\xc1\x94\x1b\xd8\xca\x41\x5a\x19\x27\x5f\x6d\xec\x89\xe5\xbf\x38\x6d\x4e\x49\xb7\xc9\x3e\xa6\xad\x1a\x73\xa6\x83\x98\xf4\xfe\x62\xc7\xd1\x6b\xa6\xf4\xd3\xa4\x25\x94\xe8\xb2\x86\xd2\x8f\xb1\xd7\xa1\x7d\xf8\xe5\xe3\x47\x7d\xdc\x61\xd2\x2d\x39\x6f\x48\xf3\x39\x92\xae\xc5\xd3\xbf\x01\x00\x00\xff\xff\x90\x3c\x0b\xfc\x6d\x07\x00\x00")

func playbookUbBytes() ([]byte, error) {
	return bindataRead(
		_playbookUb,
		"playbook.ub",
	)
}

func playbookUb() (*asset, error) {
	bytes, err := playbookUbBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "playbook.ub", size: 1901, mode: os.FileMode(436), modTime: time.Unix(1603775987, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _resourcesHelloTxtTmpl = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xf2\x48\xcd\xc9\xc9\x57\xa8\xae\x56\x50\xaa\xae\x56\xd0\xcb\x4b\xcc\x4d\x55\xa8\xad\x55\x52\xa8\xad\x55\xe4\x02\x04\x00\x00\xff\xff\xe2\x5a\x03\x64\x1b\x00\x00\x00")

func resourcesHelloTxtTmplBytes() ([]byte, error) {
	return bindataRead(
		_resourcesHelloTxtTmpl,
		"resources/hello.txt.tmpl",
	)
}

func resourcesHelloTxtTmpl() (*asset, error) {
	bytes, err := resourcesHelloTxtTmplBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "resources/hello.txt.tmpl", size: 27, mode: os.FileMode(436), modTime: time.Unix(1603775987, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"go.mod":                   goMod,
	"playbook.ub":              playbookUb,
	"resources/hello.txt.tmpl": resourcesHelloTxtTmpl,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"go.mod":      &bintree{goMod, map[string]*bintree{}},
	"playbook.ub": &bintree{playbookUb, map[string]*bintree{}},
	"resources": &bintree{nil, map[string]*bintree{
		"hello.txt.tmpl": &bintree{resourcesHelloTxtTmpl, map[string]*bintree{}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
