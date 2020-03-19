package cloudformation

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/cloudboss/go-player/pkg/lazy"
	"github.com/cloudboss/go-player/pkg/types"
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
	StackName       lazy.StringValue
	DisableRollback lazy.BoolValue
	TemplateBody    lazy.StringValue
	TemplateURL     lazy.StringValue
	cfn             *cloudformation.CloudFormation
}

func (c *CloudFormation) Initialize() error {
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	sess.Config.Logger = nil
	c.cfn = cloudformation.New(sess)

	if c.TemplateBody == nil && c.TemplateURL == nil {
		return fmt.Errorf("one of TemplateBody or TemplateURL is required")
	}

	if c.DisableRollback == nil {
		c.DisableRollback = lazy.False
	}

	return nil
}

func (c *CloudFormation) Name() string {
	return moduleName
}

func (c *CloudFormation) Build() *types.Result {
	stackInfo, err := c.getStackInfo()
	if err != nil {
		return errResult(err.Error())
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
	stackName := c.StackName()
	stackResponse, err := c.cfn.DescribeStacks(&cloudformation.DescribeStacksInput{
		StackName: &stackName,
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
	stackName := c.StackName()
	createStackInput := cloudformation.CreateStackInput{
		StackName:    &stackName,
		Capabilities: capabilities,
	}

	if c.TemplateBody != nil {
		body := c.TemplateBody()
		createStackInput.TemplateBody = &body
	}

	if c.TemplateURL != nil {
		templateURL := c.TemplateURL()
		createStackInput.TemplateURL = &templateURL
	}

	_, err := c.cfn.CreateStack(&createStackInput)
	if err != nil {
		return errResult(err.Error())
	}

	createErr := c.cfn.WaitUntilStackCreateCompleteWithContext(
		aws.BackgroundContext(),
		&cloudformation.DescribeStacksInput{StackName: &stackName},
		func(w *request.Waiter) { w.Delay = request.ConstantWaiterDelay(5 * time.Second) },
	)

	stackInfo, err := c.getStackInfo()
	if err != nil {
		return errResult(err.Error())
	}

	result := &types.Result{
		Succeeded: createErr == nil,
		Changed:   true,
		Module:    moduleName,
		Output: map[string]interface{}{
			"outputs": outputsToMap(stackInfo.Outputs),
		},
	}

	if createErr != nil {
		result.Error = *stackInfo.StackStatus
	}

	return result
}

func (c *CloudFormation) updateStack() *types.Result {
	stackName := c.StackName()
	updateStackInput := cloudformation.UpdateStackInput{
		StackName:    &stackName,
		Capabilities: capabilities,
	}

	if c.TemplateBody != nil {
		body := c.TemplateBody()
		updateStackInput.TemplateBody = &body
	}

	if c.TemplateURL != nil {
		templateURL := c.TemplateURL()
		updateStackInput.TemplateURL = &templateURL
	}

	_, err := c.cfn.UpdateStack(&updateStackInput)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			errNoUpdate := "No updates are to be performed"
			if strings.Contains(awsErr.Message(), errNoUpdate) {
				stackInfo, err := c.getStackInfo()
				if err != nil {
					return errResult(err.Error())
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
			return errResult(err.Error())
		}
		return errResult(err.Error())
	}

	updateErr := c.cfn.WaitUntilStackUpdateCompleteWithContext(
		aws.BackgroundContext(),
		&cloudformation.DescribeStacksInput{StackName: &stackName},
		func(w *request.Waiter) { w.Delay = request.ConstantWaiterDelay(5 * time.Second) },
	)

	stackInfo, err := c.getStackInfo()
	if err != nil {
		return errResult(err.Error())
	}

	result := &types.Result{
		Succeeded: updateErr == nil,
		Changed:   true,
		Module:    moduleName,
		Output: map[string]interface{}{
			"outputs": outputsToMap(stackInfo.Outputs),
		},
	}

	if updateErr != nil {
		result.Error = *stackInfo.StackStatus
	}

	return result
}

func outputsToMap(outputs []*cloudformation.Output) map[string]interface{} {
	if outputs == nil {
		return nil
	}
	outputMap := make(map[string]interface{})
	for _, output := range outputs {
		outputMap[*output.OutputKey] = *output.OutputValue
	}
	return outputMap
}

func errResult(msg string) *types.Result {
	return &types.Result{
		Succeeded: false,
		Changed:   false,
		Error:     msg,
		Module:    moduleName,
	}
}
