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
	"os"
	"testing"

	"github.com/cloudboss/unobin/pkg/playbook"
	"github.com/stretchr/testify/assert"
)

func Test_AbsolutePathAt(t *testing.T) {
	testCases := []struct {
		name     string
		cacheDir string
		path     string
		result   string
	}{
		{
			name:     "An empty path should result in an empty path",
			cacheDir: "/xyz",
			path:     "",
			result:   "",
		},
		{
			name:     "A relative path should result in an absolute path within the cache directory",
			cacheDir: "/xyz",
			path:     "abc",
			result:   "/xyz/abc",
		},
		{
			name:     "A relative path with an empty cache directory should still return an absolute path",
			cacheDir: "",
			path:     "abc",
			result:   "/abc",
		},
		{
			name:     "A relative path with a relative cache directory should result in a relative path",
			cacheDir: "/xyz",
			path:     "/abc",
			result:   "/abc",
		},
		{
			name:     "An absolute path should result in the same absolute path",
			cacheDir: "/xyz",
			path:     "/abc",
			result:   "/abc",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This environment variable is normally set by the playbook runtime.
			os.Setenv(playbook.CacheDirectoryEnv, tc.cacheDir)
			result := AbsolutePathAt(tc.path)
			assert.Equal(t, tc.result, result)
		})
	}
}
