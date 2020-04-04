package main

import (
	"github.com/cloudboss/go-player/modules/aws/cloudformation"
	"github.com/cloudboss/go-player/modules/command"
	lz "github.com/cloudboss/go-player/pkg/lazy"
	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/task"
	"github.com/cloudboss/go-player/pkg/types"
)

func main() {
	frame := &types.Frame{
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
		Frame: frame,
		Tasks: []*task.Task{
			{
				Name: `do something`,
				Module: &command.Command{
					Execute: lz.S(`ls /`)(frame),
				},
				When: task.WhenExecute(`/bin/true`),
			},
			{
				Name: `do something else`,
				Module: &command.Command{
					Execute: lz.S(`ls /`)(frame),
					Creates: lz.S(`/`)(frame),
				},
			},
			{
				Name: `build a stack`,
				Module: &cloudformation.CloudFormation{
					StackName:    lz.S(`test-stack`)(frame),
					TemplateFile: lz.Sprintf("%s", lz.Var("template")(frame)),
				},
			},
			{
				Name: `run a command from output`,
				Module: &command.Command{
					Execute: lz.Sprintf(`echo "sg1 is %s and sg2 is %s"`,
						lz.Output("build a stack", "outputs.SecurityGroup")(frame),
						lz.Output("build a stack", "outputs.SecurityGroupTwo")(frame),
					),
				},
			},
		},
	}
	pb.StartCLI()
}
