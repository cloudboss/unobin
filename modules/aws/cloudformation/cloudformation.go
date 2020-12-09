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

package cloudformation

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/cloudboss/unobin/pkg/file"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

const (
	moduleName = "cloudformation"
)

var (
	capabilities = []*string{
		aws.String("CAPABILITY_IAM"),
		aws.String("CAPABILITY_NAMED_IAM"),
		aws.String("CAPABILITY_AUTO_EXPAND"),
	}
)

type CloudFormation struct {
	StackName       string
	DisableRollback bool
	TemplateFile    string
	TemplateBody    string
	TemplateURL     string
	Parameters      map[string]interface{}
	cfn             *cloudformation.CloudFormation
}

func (c *CloudFormation) Initialize() error {
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	sess.Config.Logger = nil
	c.cfn = cloudformation.New(sess)

	if c.TemplateBody == "" && c.TemplateFile == "" && c.TemplateURL == "" {
		return fmt.Errorf("one of TemplateBody, TemplateFile, or TemplateURL is required")
	}

	if c.Parameters != nil {
		for _, v := range c.Parameters {
			_, ok := v.(string)
			if !ok {
				return errors.New("parameter values must be strings")
			}
		}
	}
	return nil
}

func (c *CloudFormation) Name() string {
	return moduleName
}

func (c *CloudFormation) Apply() *types.Result {
	stackInfo, err := c.getStackInfo()
	if err != nil {
		return util.ErrResult(err.Error(), moduleName)
	}

	stackExists := stackInfo != nil
	if !stackExists {
		return c.createStack()
	}

	return c.updateStack()
}

func (c *CloudFormation) Destroy() *types.Result {
	return nil
}

func (c *CloudFormation) getStackInfo() (*cloudformation.Stack, error) {
	stackResponse, err := c.cfn.DescribeStacks(&cloudformation.DescribeStacksInput{
		StackName: &c.StackName,
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			msgNoExist := fmt.Sprintf("Stack with id %s does not exist", c.StackName)
			if strings.Contains(awsErr.Message(), msgNoExist) {
				return nil, nil
			}
			return nil, err
		}
		return nil, err
	}

	for _, stack := range stackResponse.Stacks {
		return stack, nil
	}

	return nil, fmt.Errorf("unknown error getting stack info")
}

func (c *CloudFormation) createStack() *types.Result {
	createStackInput := cloudformation.CreateStackInput{
		StackName:    &c.StackName,
		Capabilities: capabilities,
	}
	if c.Parameters != nil {
		createStackInput.Parameters = mapToParameters(c.Parameters)
	}

	if c.TemplateBody != "" {
		createStackInput.TemplateBody = &c.TemplateBody
	}

	if c.TemplateFile != "" {
		path := file.AbsolutePathAt(c.TemplateFile)
		b, err := ioutil.ReadFile(path)
		if err != nil {
			return util.ErrResult(err.Error(), moduleName)
		}
		s := string(b)
		createStackInput.TemplateBody = &s
	}

	if c.TemplateURL != "" {
		createStackInput.TemplateURL = &c.TemplateURL
	}

	_, err := c.cfn.CreateStack(&createStackInput)
	if err != nil {
		return util.ErrResult(err.Error(), moduleName)
	}

	createErr := c.cfn.WaitUntilStackCreateCompleteWithContext(
		aws.BackgroundContext(),
		&cloudformation.DescribeStacksInput{StackName: &c.StackName},
		func(w *request.Waiter) {
			w.Delay = request.ConstantWaiterDelay(10 * time.Second)
			w.MaxAttempts = 1080
		},
	)

	result := &types.Result{
		Succeeded: createErr == nil,
		Changed:   true,
		Module:    moduleName,
	}

	if createErr != nil {
		result.Error = createErr.Error()
	} else {
		stackInfo, err := c.getStackInfo()
		if err != nil {
			result.Succeeded = false
			result.Error = err.Error()
			return result
		}
		if stackInfo.Outputs != nil {
			result.Output = map[string]interface{}{
				"outputs": outputsToMap(stackInfo.Outputs),
			}
		}
	}

	return result
}

func (c *CloudFormation) updateStack() *types.Result {
	updateStackInput := cloudformation.UpdateStackInput{
		StackName:    &c.StackName,
		Capabilities: capabilities,
	}
	if c.Parameters != nil {
		updateStackInput.Parameters = mapToParameters(c.Parameters)
	}

	if c.TemplateBody != "" {
		updateStackInput.TemplateBody = &c.TemplateBody
	}

	if c.TemplateFile != "" {
		b, err := ioutil.ReadFile(c.TemplateFile)
		if err != nil {
			return util.ErrResult(err.Error(), moduleName)
		}
		s := string(b)
		updateStackInput.TemplateBody = &s
	}

	if c.TemplateURL != "" {
		updateStackInput.TemplateURL = &c.TemplateURL
	}

	_, err := c.cfn.UpdateStack(&updateStackInput)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			errNoUpdate := "No updates are to be performed"
			if strings.Contains(awsErr.Message(), errNoUpdate) {
				stackInfo, err := c.getStackInfo()
				if err != nil {
					return util.ErrResult(err.Error(), moduleName)
				}
				return &types.Result{
					Succeeded: true,
					Changed:   false,
					Module:    moduleName,
					Output: map[string]interface{}{
						"outputs": outputsToMap(stackInfo.Outputs),
					},
				}
			}
			return util.ErrResult(err.Error(), moduleName)
		}
		return util.ErrResult(err.Error(), moduleName)
	}

	updateErr := c.cfn.WaitUntilStackUpdateCompleteWithContext(
		aws.BackgroundContext(),
		&cloudformation.DescribeStacksInput{StackName: &c.StackName},
		func(w *request.Waiter) {
			w.Delay = request.ConstantWaiterDelay(10 * time.Second)
			w.MaxAttempts = 1080
		},
	)

	result := &types.Result{
		Succeeded: updateErr == nil,
		Changed:   true,
		Module:    moduleName,
	}

	if updateErr != nil {
		result.Error = updateErr.Error()
	} else {
		stackInfo, err := c.getStackInfo()
		if err != nil {
			result.Succeeded = false
			result.Error = err.Error()
			return result
		}
		if stackInfo.Outputs != nil {
			result.Output = map[string]interface{}{
				"outputs": outputsToMap(stackInfo.Outputs),
			}
		}
	}

	return result
}

func outputsToMap(outputs []*cloudformation.Output) map[string]interface{} {
	outputMap := make(map[string]interface{})
	for _, output := range outputs {
		outputMap[*output.OutputKey] = *output.OutputValue
	}
	return outputMap
}

func mapToParameters(m map[string]interface{}) []*cloudformation.Parameter {
	parameters := make([]*cloudformation.Parameter, len(m))
	i := 0
	for k, v := range m {
		value := v.(string)
		parameters[i] = &cloudformation.Parameter{
			ParameterKey:   &k,
			ParameterValue: &value,
		}
		i++
	}
	return parameters
}
