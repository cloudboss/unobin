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

package compiler

import (
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_grammarFiles(t *testing.T) {
	testCases := []struct {
		name string
		file string
	}{
		{
			name: "Just spaces are fine",
			file: "resources/spaces.ub",
		},
		{
			name: "Arrays work",
			file: "resources/arrays.ub",
		},
		{
			name: "Bools work",
			file: "resources/bools.ub",
		},
		{
			name: "Indexes work",
			file: "resources/indexes.ub",
		},
		{
			name: "Functions work",
			file: "resources/functions.ub",
		},
		{
			name: "Numbers work",
			file: "resources/numbers.ub",
		},
		{
			name: "Objects work",
			file: "resources/objects.ub",
		},
		{
			name: "Strings work",
			file: "resources/strings.ub",
		},
		{
			name: "Full playbooks work",
			file: "resources/playbook.ub",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ioutil.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
				return
			}
			g := Grammar{Buffer: string(b)}
			g.Init()
			assert.Nil(t, g.Parse())
		})
	}
}

func Test_grammarInline(t *testing.T) {
	testCases := []struct {
		name   string
		lang   string
		errStr string
	}{
		{
			name:   "Bracket indexes do not start with spaces",
			lang:   `index: out[ name cannot start with space].attr`,
			errStr: "parse error near space",
		},
		{
			name:   "Bracket indexes do not end with spaces",
			lang:   `index: out[name cannot end with space ].attr`,
			errStr: "parse error near INDEX_CLOSE",
		},
		{
			name:   "Bracket indexes do not contain hash",
			lang:   `index: variable[I can @@ include just about !!%$ anything but not #].attr`,
			errStr: "parse error near not_newline",
		},
		{
			name: "Bracket indexes do not contain newline",
			lang: `index: variable[I can @@ include just about !!%$ anything but not a
newline].attr`,
			errStr: "parse error near newline",
		},
		{
			name:   "Dot indexes do not start with spaces",
			lang:   `index: out. name.attr`,
			errStr: "parse error near DOT",
		},
		{
			name:   "Dot indexes do not end with spaces",
			lang:   `index: out.name .attr`,
			errStr: "parse error near space",
		},
		{
			name:   "Dot indexes do not contain spaces",
			lang:   `index: variable.thing with spaces.attr`,
			errStr: "parse error near space",
		},
		{
			name:   "Dot indexes do not contain special characters",
			lang:   `index: variable.some@special$characters!isn't&good.attr`,
			errStr: "parse error near alpha",
		},
		{
			name:   "Strings do not use double quotes",
			lang:   `error-string: "Strings use single, not double, quotes"`,
			errStr: "parse error near space",
		},
		{
			name: "Strings must be on one line",
			lang: `error-string: 'Strings do not contain
newlines'`,
			errStr: "parse error near newline",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := Grammar{Buffer: tc.lang}
			g.Init()
			err := g.Parse()
			if err != nil {
				t.Logf("%s\n", err.Error())
			}
			if tc.errStr != "" {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tc.errStr)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
