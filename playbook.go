package main

import (
	"io/ioutil"
	"os"

	"github.com/cloudboss/go-player/modules/aws/cloudformation"
	"github.com/cloudboss/go-player/modules/command"
	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/task"
	"github.com/cloudboss/go-player/pkg/types"
)

func s(s string) types.StringF {
	return func() string {
		return s
	}
}

func main() {
	b, err := ioutil.ReadFile("cf.yml")
	if err != nil {
		os.Exit(1)
	}
	pb := playbook.Playbook{
		Tasks: []*task.Task{
			{
				Name: `do something`,
				Module: &command.Command{
					Execute: s(`ls /`),
				},
				When: task.WhenExecute(`/bin/true`),
			},
			{
				Name: `do something else`,
				Module: &command.Command{
					Execute: s(`ls /`),
					Creates: s(`/`),
				},
			},
			{
				Name: `build a stack`,
				Module: &cloudformation.CloudFormation{
					StackName:    `test-stack`,
					TemplateBody: string(b),
				},
			},
		},
	}
	pb.Run()
	if !pb.Succeeded {
		os.Exit(1)
	}
	os.Exit(0)
}
