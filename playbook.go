package main

import (
	"github.com/cloudboss/go-player/modules/aws/cloudformation"
	"github.com/cloudboss/go-player/modules/command"
	"github.com/cloudboss/go-player/pkg/lazy"
	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/task"
	"github.com/cloudboss/go-player/pkg/types"
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
			"$id":     "github.com/cloudboss/go-player",
			"type":    "object",
			"properties": map[string]interface{}{
				"template": map[string]interface{}{"type": "string"},
			},
			"additionalProperties": false,
		},
		Context: ctx,
		Tasks: []*task.Task{
			{
				Name: `do something`,
				Module: &command.Command{
					Execute: lazy.S(`ls /`)(ctx),
				},
				When: lazy.WhenExecute(lazy.S(`/bin/true`)(ctx))(ctx),
			},
			{
				Name: `do something else`,
				Module: &command.Command{
					Execute: lazy.S(`ls /`)(ctx),
					Creates: lazy.S(`/`)(ctx),
				},
			},
			{
				Name: `build a stack`,
				Module: &cloudformation.CloudFormation{
					StackName:    lazy.S(`test-stack`)(ctx),
					TemplateFile: lazy.StringVar(lazy.S("template")(ctx))(ctx),
				},
			},
			{
				Name: `run a command from output`,
				Module: &command.Command{
					Execute: lazy.Format(lazy.S(`echo "sg1 is %s and sg2 is %s"`)(ctx),
						lazy.AnyOutput(lazy.S("build a stack")(ctx), lazy.S("outputs.SecurityGroup")(ctx))(ctx),
						lazy.AnyOutput(lazy.S("build a stack")(ctx), lazy.S("outputs.SecurityGroupTwo")(ctx))(ctx),
					)(ctx),
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
