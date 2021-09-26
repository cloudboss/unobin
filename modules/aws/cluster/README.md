# cluster

Generate an opinionated CloudFormation template for a cluster composed of an autoscaling group, optionally with load balancers. The `template` output should be passed to the CloudFormation module.

# Arguments

`stack-name`: (Required, type _string_) - The name given to the stack. Note that the stack name must also be given when creating the stack with e.g. the `cloudformation` Unobin module; this is used by the `cluster` module for naming resources in the generated template.

`machines`: (Required, type _object_ of _string_ -> _any_) - Object describing the machines in the autoscaling group (see [machines](#machines)).

`vpc-id`: (Required, type _string_) - ID of VPC where machines and load balancers will be located.

`load-balancers`: (Optional, type _array_ of _object_) - Array of objects each describing a load balancer (see [load-balancer](#load-balancer)). This cannot be set if `machines.target-group-arns` is defined.

`format`: (Optional, type _string_, default `yaml`) - Format of generated CloudFormation template, must be one of `json` or `yaml`.

## machines

`image-id`: (Required, type _string_) - ID of AMI used to launch machines.

`instance-type`: (Required, type _string_) - Instance type of machines.

`key-name`: (Required, type _string_) - SSH key name assigned to machines.

`min-count`: (Required, type _int_) - Minimum number of instances in the autoscaling group.

`max-count`: (Required, type _int_) - Maximum number of instances in the autoscaling group.

`subnets`: (Required, type _array_ of _string_) - Array of subnet IDs in which machines will be placed.

`block-device-mappings`: (Optional, type _array_ of _object_) - Array of objects each describing a block device mapping (see [block-device-mapping](#block-device-mapping)).

`creation-policy`: (Optional, type _object_) - An object describing a creation policy for the machines (see [creation-policy](#creation-policy)).

`egress-rules`: (Optional, type _array_ of _object_) - An array of objects each describing an egress rule (see [egress-rule](#egress-rule)).

`extra-security-groups`: (Optional, type _array_ of _string_) - Extra security groups assigned to the autoscaling group.

`extra-tags`: (Optional, type _object_ of _string_ -> _string_) - Extra tags to assign to the autoscaling group. These will be propagated to the instances.

`iam-instance-profile`: (Optional, type _string_) - Name of IAM instance profile assigned to machines.

`ingress-rules`: (Optional, type _array_ of _object_) - An array of objects each describing an ingress rule (see [ingress-rule](#ingress-rule)).

`placement-tenancy`: (Optional, type _string_, default `default`) - TODO: fix

`target-group-arns`: (Optional, type _array_ of _string_) - An array of Target Group ARNs to assign to machines. This cannot be set if `load-balancers` is defined.

`update-policy`: (Optional, type _object_) - An update policy for when updates are applied to the autoscaling group (see [update-policy](#update-policy)).

`user-data`: (Optional, type _string_) - User data added to machines.

## block-device-mapping

`device-name`: (Optional, type _string_) - Name of block device, for example `/dev/sdh` or `xvdh`.

`ebs`: (Optional, type _object_) - An object describing an EBS volume (see [ebs](#ebs)).

`no-device`: (Optional, type _string_) - To omit the device from the block device mapping, specify an empty string.

`virtual-name`: (Optional, type _string_) - The virtual device name for instance store volumes.

## ebs

`delete-on-termination`: (Optional, type _bool_) - Whether or not to delete the volume when the machine is terminated.

`encrypted`: (Optional, type _bool_) - Whether or not the volume is encrypted.

`iops`: (Optional, type _int_) - Number of IOPs of volume, supported when `volume-type` is `io1`, `io2`, or `gp3`.

`kms-key-id`: (Optional, type _string_) - ARN of KMS key used to encrypt the volume.

`snapshot-id`: (Optional, type _string_) - ID of snapshot from which volume will be created.

`volume-size`: (Optional, type _int_) - Size of volume. Not used if `snapshot-id` is defined.

`volume-type`: (Optional, type _string_) - Type of volume, may be one of `gp2`, `gp3`, `io1`, `io2`, `sc1`, `st1`, or `standard`.

## creation-policy

`auto-scaling-creation-policy`: (Optional, type _object_) - An object describing the percentage of instances that must signal success for a replacing update (see [auto-scaling-creation-policy](#auto-scaling-creation-policy)).

`resource-signal`: (Optional, type _object_) - An object describing success signals during stack creation (see [resource-signal](#resource-signal)).

## auto-scaling-creation-policy

`min-successful-instances-percent`: (Optional, type _int_, default `100`) - The minimum percentage of instances that must signal success.

## resource-signal

`count`: (Optional, type _int_) - The number of success signals CloudFormation must receive. Defaults to the value of `machines.min-count`.

`timeout`: (Optional, type _int_, `PT15M`) - The duration that CloudFormation waits for the number of signals defined in `count`, defined as an [ISO 8601 duration](https://en.wikipedia.org/wiki/ISO_8601#Durations).

## egress-rule

`ip-protocol`: (Required, type _string_) - The IP protocol or [number](http://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml). Use `-1` to specify all protocols.

`cidr-ip`: (Conditional, type _string_) - The IPv4 destination CIDR range. One of `cidr-ip`, `cidr-ipv6`, `destination-prefix-list-id`, or `destination-security-group-id` must be defined.

`cidr-ipv6`: (Conditional, type _string_) - The IPv6 destination CIDR range. One of `cidr-ip`, `cidr-ipv6`, `destination-prefix-list-id`, or `destination-security-group-id` must be defined.

`destination-prefix-list-id`: (Conditional, type _string_) - The destination prefix list ID. One of `cidr-ip`, `cidr-ipv6`, `destination-prefix-list-id`, or `destination-security-group-id` must be defined.

`destination-security-group-id`: (Conditional, type _string_) - The destination security group ID. One of `cidr-ip`, `cidr-ipv6`, `destination-prefix-list-id`, or `destination-security-group-id` must be defined.

`from-port`: (Optional, type _int_) - The starting port for the rule.

`to-port`: (Optional, type _int_) - The ending port for the rule.

## ingress-rule

`ip-protocol`: (Required, type _string_) - The IP protocol or [number](http://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml). Use `-1` to specify all protocols.

`cidr-ip`: (Conditional, type _string_) - The IPv4 source CIDR range. One of `cidr-ip`, `cidr-ipv6`, or `source-security-group-id` must be defined.

`cidr-ipv6`: (Conditional, type _string_) - The IPv6 source CIDR range. One of `cidr-ip`, `cidr-ipv6`, or `source-security-group-id` must be defined.

`source-security-group-id`: (Conditional, type _string_) - The source security group ID. One of `cidr-ip`, `cidr-ipv6`, or `source-security-group-id` must be defined.

`source-security-group-owner-id`: (Conditional, type _string_) - The AWS account ID that owns the source security group. This is required if `source-security-group-id` is defined and it lives in a different account than the one creating the stack.

`from-port`: (Optional, type _int_) - The starting port for the rule.

`to-port`: (Optional, type _int_) - The ending port for the rule.

## update-policy

`auto-scaling-replacing-update`: (Conditional, type _object_) - An object describing an autoscaling replacing update (see [auto-scaling-replacing-update](#auto-scaling-replacing-update)). One of `auto-scaling-replacing-update`, `auto-scaling-rolling-update`, or `auto-scaling-scheduled-action` must be defined.

`auto-scaling-rolling-update`: (Conditional, type _object_) - An object describing an autoscaling rolling update (see [auto-scaling-rolling-update](#auto-scaling-rolling-update)). One of `auto-scaling-replacing-update`, `auto-scaling-rolling-update`, or `auto-scaling-scheduled-action` must be defined.

`auto-scaling-scheduled-action`: (Conditional, type _object_) - An object describing an autoscaling scheduled action (see [auto-scaling-scheduled-action](#auto-scaling-scheduled-action)). One of `auto-scaling-replacing-update`, `auto-scaling-rolling-update`, or `auto-scaling-scheduled-action` must be defined.

## auto-scaling-replacing-update

`will-replace`: (Required, type _bool_) - Specifies whether an autoscaling group and the instances it contains are replaced during an update.

## auto-scaling-rolling-update

`max-batch-size`: (Optional, type _int_, default `1`) - The maximum number of instances to be updated at a time.

`min-instances-in-service`: (Optional, type _int_, default `0`) - The number of instances that must be in service during an update.

`min-successful-instances-percent`: (Optional, type _int_, default `100`) - The percentage of instances that must send a success signal for an update to succeed.

`pause-time`: (Optional, type _string_, default `PT0S`) - The pause duration between updates to an autoscaling group, defined as an [ISO 8601 duration](https://en.wikipedia.org/wiki/ISO_8601#Durations).

`suspend-processes`: (Optional, type _array_ of _string_) - An array of [processes to suspend](https://docs.aws.amazon.com/autoscaling/ec2/APIReference/API_SuspendProcesses.html) during an update.

`wait-on-resource-signals`: (Optional, type _bool_, default `false`) - Whether or not CloudFormation should wait for new instances to send a success signal before proceeding to the next batch.

## auto-scaling-scheduled-action

`ignore-unmodified-group-size-properties`: (Optional, type _bool_, default `false`) - Whether or not CloudFormation should ignore size differences between the current autoscaling group and what is defined in the template during a stack update.

## load-balancer

`listeners`: (Required, type _array_ of _object_) - An array of listener objects (see [listener](#listener)).

`subnets`: (Required, type _array_ of _string_) - An array of subnet IDs.

`type`: (Required, type _string_) - Type of load balancer, must be one of `application` or `network`.

`attributes`: (Optional, type _array_ of _object_) - An array of attribute objects (see [attribute](#attribute)). Available attributes are listed in the [AWS documentation](https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/API_LoadBalancerAttribute.html).

`dns`: (Optional, type _object_) - An object describing DNS properties on the load balancer (see [dns](#dns)).

`egress-rules`: (Optional, type _array_ of _object_) - An array of objects each describing an egress rule (see [egress-rule](#egress-rule)).

`ingress-rules`: (Optional, type _array_ of _object_) - An array of objects each describing an ingress rule (see [ingress-rule](#ingress-rule)).

`scheme`: (Optional, type _string_, default `internal`) - Load balancer scheme, must be one of `internal` or `internet-facing`.

## attribute

`key`: (Required, type _string_) - The attribute key.

`value`: (Required, type _string_) - The attribute value.

## dns

`domain`: (Required, type _string_) - The DNS domain to which the load balancer hostname should be added. A hosted zone for the domain must already be present in Route 53.

`hostname`: (Required, type _string_) - The hostname to add to the DNS domain.

## listener

`listen-port`: (Required, type _int_) - The port on which the load balancer is listening.

`listen-protocol`: (Required, type _string_) - The protocol for connections to the load balancer.

`health-check`: (Required, type _object_) - An object describing the health check (see [health-check](#health-check)).

`target-protocol`: (Required, type _string_) - The protocol on which target machines are listening.

`target-port`: (Optional, type _int_) - The port which the load balancer sends traffic to on the target hosts.

`target-group-attributes`: (Optional, type _array_ of _object_) - An array of attribute objects (see [attribute](#attribute)). Available attributes are listed in the [AWS documentation](https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/API_TargetGroupAttribute.html).

`certificate-arn`: (Optional, type _string_) - ARN of [ACM](https://docs.aws.amazon.com/acm/latest/userguide/acm-overview.html) SSL certificate.

`ssl-policy`: (Optional, type _string_, default `ELBSecurityPolicy-2016-08`) - SSL policy when `listen-protocol` is `HTTPS` or `TLS`.

## health-check

`port`: (Required, type _int_) - The port on which health checks are done on target machines.

`protocol`: (Optional, type _string_) - The protocol used for health checks on target machines. Defaults to the listener's `target-protocol`.

`path`: (Optional, type _string_, default `/`) - The HTTP path on which health checks are done, when the listener's `listen-protocol` is `HTTP` or `HTTPS`.

`matcher`: (Optional, type _int_) - The HTTP code to match for responses from the health check, when `protocol` is `HTTP` or `HTTPS`.

# Outputs

`template`: (type _string_) - The generated CloudFormation template.

# Example

Example usage in a playbook:

```
imports {
  cloudformation: 'github.com/cloudboss/unobin/modules/aws/cloudformation.CloudFormation'
  cluster: 'github.com/cloudboss/unobin/modules/aws/cluster.Cluster'
  readfile: 'github.com/cloudboss/unobin/modules/file/readfile.ReadFile'
}

task [read user data] {
  module: readfile
  args: { src: 'user-data.txt' }
}

task [generate cloudformation template] {
  module: cluster
  args: {
    stack-name: string-var.stack-name
    machines: {
      block-device-mappings: [{
        device-name: '/dev/sdf'
        ebs: {
          delete-on-termination: true
          volume-size: 10
          volume-type: 'gp2'
        }
      }]
      image-id: 'ami-0f2c5742058f780c3'
      ingress-rules: [
        {
          cidr-ip: '0.0.0.0/0'
          from-port: 22
          to-port: 22
        }
      ]
      instance-type: 'm5.large'
      key-name: 'abc'
      max-count: 10
      min-count: 2
      subnets: ['subnet-0f9246d5bb46a52db' 'subnet-01d20c0ce7738a63d']
      user-data: { content: string-output[read user data].content }
    }
    vpc-id: 'vpc-013279c1e2c674ace'
  }
}

task [ensure abc cluster stack is present] {
  module: cloudformation
  args: {
    stack-name: string-var.stack-name
    template-body: string-output[generate cloudformation template].template
  }
}
```
