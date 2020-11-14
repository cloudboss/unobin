package main

import (
	"github.com/cloudboss/unobin/modules/aws/cloudformation"
	"github.com/cloudboss/unobin/modules/command"
	"github.com/cloudboss/unobin/pkg/functions"
	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/playbook"
	"github.com/cloudboss/unobin/pkg/task"
	"github.com/cloudboss/unobin/pkg/types"
)

func main() {
	ctx := &types.Context{
		Vars:  map[string]interface{}{},
		State: map[string]interface{}{},
	}

	pb := playbook.Playbook{
		Name:        "cfer",
		Description: "Build a CloudFormation stack",
		InputSchema: map[string]interface{}{
			"$schema": "http://json-schema.org/schema#",
			"$id":     "github.com/cloudboss/unobin",
			"type":    "object",
			"properties": map[string]interface{}{
				"stack-name":       map[string]interface{}{"type": "string"},
				"template":         map[string]interface{}{"type": "string"},
				"disable-rollback": map[string]interface{}{"type": "boolean"},
			},
			"additionalProperties": false,
		},
		Context: ctx,
		Tasks: []*task.Task{
			{
				Description: `do something`,
				UnwrapModule: func() (module.Module, error) {
					mod := &command.Command{}
					mod.Execute = "ls /"
					return mod, nil
				},
				When: func() (bool, error) {
					when := functions.WhenExecute(ctx, functions.String{"/bin/true", nil})
					return when.Value, when.Error
				},
				Context: ctx,
			},
			{
				Description: `do something else`,
				UnwrapModule: func() (module.Module, error) {
					mod := &command.Command{}
					mod.Execute = "ls /"
					mod.Creates = "/"
					return mod, nil
				},
				Context: ctx,
			},
			{
				Description: `build a stack`,
				UnwrapModule: func() (module.Module, error) {
					mod := &cloudformation.CloudFormation{}
					stackName := functions.StringVar(ctx, functions.String{"stack-name", nil})
					if stackName.Error != nil {
						return mod, stackName.Error
					}
					mod.StackName = stackName.Value
					templateFile := functions.StringVar(ctx, functions.String{"template", nil})
					if templateFile.Error != nil {
						return mod, templateFile.Error
					}
					mod.TemplateFile = templateFile.Value
					disableRollback := functions.BoolVar(ctx, functions.String{"disable-rollback", nil})
					if disableRollback.Error != nil {
						return mod, disableRollback.Error
					}
					mod.DisableRollback = disableRollback.Value
					return mod, nil
				},
				Context: ctx,
			},
			{
				Description: `compound task`,
				Body: []*task.Task{
					{
						Description: `run a command from output`,
						UnwrapModule: func() (module.Module, error) {
							mod := &command.Command{}
							execute := functions.Format(ctx,
								functions.String{`echo "sg1 is %s and sg2 is %s"`, nil},
								functions.AnyOutput(ctx,
									functions.String{"build a stack", nil},
									functions.String{"outputs.SecurityGroup", nil}),
								functions.AnyOutput(ctx,
									functions.String{"build a stack", nil},
									functions.String{"outputs.SecurityGroupTwo", nil}))
							if execute.Error != nil {
								return mod, execute.Error
							}
							mod.Execute = execute.Value
							return mod, nil
						},
						Context: ctx,
					},
					{
						Description: `run just a simple command`,
						UnwrapModule: func() (module.Module, error) {
							mod := &command.Command{}
							mod.Execute = "ls /etc"
							return mod, nil
						},
						Context: ctx,
					},
					{
						Description: `a nested compound module`,
						Body: []*task.Task{
							{
								Description: `run an even simpler command`,
								UnwrapModule: func() (module.Module, error) {
									mod := &command.Command{}
									// This nonexistent command should fail.
									mod.Execute = "abcxyz123"
									return mod, nil
								},
								Context: ctx,
							},
						},
						Context: ctx,
					},
				},
				Rescue: []*task.Task{
					{
						Description: `recover`,
						UnwrapModule: func() (module.Module, error) {
							mod := &command.Command{}
							mod.Execute = "true"
							return mod, nil
						},
						Context: ctx,
					},
				},
				Context: ctx,
			},
		},
	}
	pb.StartCLI()
}
