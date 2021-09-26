# cloudformation

Create or update a CloudFormation stack.

# Arguments

`stack-name`: (Required, type _string_) - Name given to the CloudFormation stack.

`template-file`: (Conditional, type _string_) - Path to a CloudFormation template file. One of `template-file`, `template-body`, or `template-url` must be present.

`template-body`: (Conditional, type _string_) - A literal CloudFormation template as a string. One of `template-file`, `template-body`, or `template-url` must be present.

`template-url`: (Conditional, type _string_) - The URL of a CloudFormation template. One of `template-file`, `template-body`, or `template-url` must be present.

`disable-rollback`: (Optional, type _bool_, default `false`) - If `true`, stack will not rollback in case of errors.

`parameters`: (Optional, type _object_ of _string_ -> _any_) - Parameters to pass to the stack, corresponding to the parameters defined in the `Parameters` section of the CloudFormation template.

# Outputs

`outputs`: (type _object_ of _string_ -> _any_) - Stack outputs as string keys mapped to any value, as defined in the CloudFormation template.

# Example

Example usage in a playbook:

```
imports {
  cloudformation: 'github.com/cloudboss/unobin/modules/aws/cloudformation.CloudFormation'
  template: 'github.com/cloudboss/unobin/modules/file/template.Template'
}

task [expand template for cloudformation stack] {
  module: template
  args: {
    src: 'cf-vpc.yml.template'
    dest: '/tmp/cf-vpc.yml'
    vars: {
      'region': 'us-west-2'
      'vpc-name': 'xyz'
      'cidr-block': '10.57.80.0/20'
    }
  }
}

task [ensure xyz vpc stack is present] {
  module: cloudformation
  args: {
    stack-name: 'vpc-xyz'
    template-file: '/tmp/cf-vpc.yml'
  }
}
```
