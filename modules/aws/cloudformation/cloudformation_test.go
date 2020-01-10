package cloudformation

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/stretchr/testify/assert"
)

func Test_outputsToMap(t *testing.T) {
	testCases := []struct {
		name    string
		outputs []*cloudformation.Output
		result  map[string]interface{}
	}{
		{
			name:    "nil output array should produce a nil map",
			outputs: nil,
			result:  nil,
		},
		{
			name:    "empty output array should produce an empty map",
			outputs: []*cloudformation.Output{},
			result:  map[string]interface{}{},
		},
		{
			name: "nonempty output array should produce a nonempty map",
			outputs: []*cloudformation.Output{
				{
					Description: aws.String("whocares"),
					ExportName:  aws.String("whocares"),
					OutputKey:   aws.String("abc"),
					OutputValue: aws.String("xyz"),
				},
			},
			result: map[string]interface{}{
				"abc": "xyz",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := outputsToMap(tc.outputs)
			assert.Equal(t, result, tc.result)
		})
	}
}
