package main

import (
	"io/ioutil"
	"os"

	"github.com/cloudboss/go-player/modules/aws/cloudformation"
	"github.com/cloudboss/go-player/modules/command"
	lz "github.com/cloudboss/go-player/pkg/lazy"
	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/task"
)

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
					Execute: lz.S(`ls /`),
				},
				When: task.WhenExecute(`/bin/true`),
			},
			{
				Name: `do something else`,
				Module: &command.Command{
					Execute: lz.S(`ls /`),
					Creates: lz.S(`/`),
				},
			},
			{
				Name: `build a stack`,
				Module: &cloudformation.CloudFormation{
					StackName:    lz.S(`test-stack`),
					TemplateBody: lz.S(string(b)),
				},
			},
			{
				Name: `run a command from output`,
				Module: &command.Command{
					Execute: lz.Sprintf(`echo "sg1 is %s and sg2 is %s"`,
						lz.Output("build a stack", "outputs.SecurityGroup"),
						lz.Output("build a stack", "outputs.SecurityGroupTwo"),
					),
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
