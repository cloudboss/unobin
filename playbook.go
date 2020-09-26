package main

import (
	"github.com/cloudboss/unobin/modules/aws/cloudformation"
	"github.com/cloudboss/unobin/modules/command"
	"github.com/cloudboss/unobin/pkg/lazy"
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
				"stack_name": map[string]interface{}{"type": "string"},
				"template":   map[string]interface{}{"type": "string"},
			},
			"additionalProperties": false,
		},
		Context: ctx,
		Tasks: []*task.Task{
			{
				Name: `do something`,
				Unwrap: func() (module.Module, error) {
					mod := &command.Command{}
					execute, err := lazy.S(`ls /`)(ctx)()
					if err != nil {
						return mod, err
					}
					mod.Execute = execute
					return mod, nil
				},
				When: lazy.WhenExecute(lazy.S(`/bin/true`)(ctx))(ctx),
			},
			{
				Name: `do something else`,
				Unwrap: func() (module.Module, error) {
					mod := &command.Command{}
					execute, err := lazy.S(`ls /`)(ctx)()
					if err != nil {
						return mod, err
					}
					mod.Execute = execute
					creates, err := lazy.S(`/`)(ctx)()
					if err != nil {
						return mod, err
					}
					mod.Creates = creates
					return mod, nil
				},
			},
			{
				Name: `build a stack`,
				Unwrap: func() (module.Module, error) {
					mod := &cloudformation.CloudFormation{}
					stackName, err := lazy.StringVar(lazy.S("stack_name")(ctx))(ctx)()
					if err != nil {
						return mod, err
					}
					mod.StackName = stackName
					templateFile, err := lazy.StringVar(lazy.S("template")(ctx))(ctx)()
					if err != nil {
						return mod, err
					}
					mod.TemplateFile = templateFile
					return mod, nil
				},
			},
			{
				Name: `run a command from output`,
				Unwrap: func() (module.Module, error) {
					mod := &command.Command{}
					execute, err := lazy.Format(lazy.S(`echo "sg1 is %s and sg2 is %s"`)(ctx),
						lazy.AnyOutput(lazy.S("build a stack")(ctx), lazy.S("outputs.SecurityGroup")(ctx))(ctx),
						lazy.AnyOutput(lazy.S("build a stack")(ctx), lazy.S("outputs.SecurityGroupTwo")(ctx))(ctx),
					)(ctx)()
					if err != nil {
						return mod, err
					}
					mod.Execute = execute
					return mod, nil
				},
			},
		},
		Outputs: map[string]interface{}{
			"sg1": lazy.AnyOutput(lazy.S("build a stack")(ctx), lazy.S("outputs.SecurityGroup")(ctx))(ctx),
			"sg2": lazy.AnyOutput(lazy.S("build a stack")(ctx), lazy.S("outputs.SecurityGroupTwo")(ctx))(ctx),
		},
	}
	pb.StartCLI()
}
