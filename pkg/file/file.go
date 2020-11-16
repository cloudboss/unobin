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

package file

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// WriteOnChange atomically writes path if contents differs from the file contents.
// The returned bool indicates if the file at path was changed.
func WriteOnChange(path string, contents *bytes.Buffer, mode os.FileMode) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			err = atomicWrite(path, contents, mode)
			if err != nil {
				return false, err
			}
			_, err = chmodOnChange(path, mode)
			if err != nil {
				return true, err
			}
			return true, nil
		}
		return false, err
	}

	bites := contents.Bytes()

	same, err := diff(path, bites)
	if err != nil {
		return false, err
	}
	if same {
		return chmodOnChange(path, mode)
	}

	err = atomicWrite(path, bytes.NewReader(bites), mode)
	if err != nil {
		return false, err
	}

	_, err = chmodOnChange(path, mode)
	return true, err
}

// atomicWrite creates a temporary directory in the same directory as the file at
// path, writes contents to a file there, and then renames the temporary file to
// the path. The temporary directory is removed upon return.
// func AtomicWrite(path string, contents []byte, mode os.FileMode) error {
func atomicWrite(path string, contents io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tempDir, err := ioutil.TempDir(dir, ".unobin")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	base := filepath.Base(path)
	tempFilePath := fmt.Sprintf("%s/%s", tempDir, base)
	fd, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}
	defer fd.Close()

	_, err = io.Copy(fd, contents)
	if err != nil {
		return err
	}

	return os.Rename(tempFilePath, path)
}

// diff returns true if the file at path is the same as contents.
func diff(path string, contents []byte) (bool, error) {
	fd, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer fd.Close()

	fileChecksum, err := sha256Sum(fd)
	if err != nil {
		return false, err
	}

	contentsChecksum, err := sha256Sum(bytes.NewReader(contents))
	if err != nil {
		return false, err
	}

	if bytes.Equal(fileChecksum, contentsChecksum) {
		return true, nil
	}

	return false, nil
}

// chmodOnChange changes the mode of the file at path if it differs from mode.
// The returned bool indicates if the mode was changed.
func chmodOnChange(path string, mode os.FileMode) (bool, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if stat.Mode() != mode {
		err = os.Chmod(path, mode)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func sha256Sum(reader io.Reader) ([]byte, error) {
	hash := sha256.New()
	_, err := io.Copy(hash, reader)
	if err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}
