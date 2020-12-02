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

package functions

import (
	"encoding/base64"
	"testing"

	"github.com/cloudboss/unobin/pkg/types"
	"github.com/stretchr/testify/assert"
)

func Test_B64Decode(t *testing.T) {
	ctx := &types.Context{}
	testCases := []struct {
		name   string
		in     String
		result String
	}{
		{
			name:   "empty string should produce empty string",
			in:     String{"", nil},
			result: String{"", nil},
		},
		{
			name:   "nonempty string should decode",
			in:     String{"VW5vYmluIGlzIHRoZSBib3Nz", nil},
			result: String{"Unobin is the boss", nil},
		},
		{
			name:   "invalid base 64 string should return an error",
			in:     String{"abcxyz123", nil},
			result: String{"", base64.CorruptInputError(8)},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := B64Decode(ctx, tc.in)
			assert.Equal(t, tc.result, result)
		})
	}
}

func Test_B64Encode(t *testing.T) {
	ctx := &types.Context{}
	testCases := []struct {
		name   string
		in     String
		result String
	}{
		{
			name:   "empty string should produce empty string",
			in:     String{"", nil},
			result: String{"", nil},
		},
		{
			name:   "nonempty string should encode",
			in:     String{"Unobin is the boss", nil},
			result: String{"VW5vYmluIGlzIHRoZSBib3Nz", nil},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := B64Encode(ctx, tc.in)
			assert.Equal(t, tc.result, result)
		})
	}
}
