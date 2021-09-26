# sqldb

Generate an opinionated CloudFormation template for Amazon RDS instances. The `template` output should be passed to the CloudFormation module.

# Arguments

`database-name`: (Required, type _string_) - The logical name of the database.

`engine`: (Required, type _string_) - The database engine.

`engine-version`: (Required, type _string_) - The database engine version.

`instance-class`: (Conditional, type _string_) - The class of the instance, required unless `engine-mode` is `serverless`.

`allow-major-version-upgrade`: (Optional, type _bool_, default `false`) - Whether or not to allow major version upgrades on updates.

`auto-minor-version-upgrade`: (Optional, type _bool_, default `false`) - Whether or not to minor version upgrades should be applied automatically during the maintenance window.

`backup-retention-period`: (Optional, type _int_) - The number of days for which automated backups are retained, must be between `1` and `35`.

`copy-tags-to-snapshot`: (Optional, type _bool_) - Whether or not to copy tags from the database instance to snapshots.

`dns`: (Optional, type _object_) - An object describing DNS for the database (see [dns](#dns)).

`engine-mode`: (Optional, type _string_) - The database engine mode.

`format`: (Optional, type _string_, default `yaml`) - Format of the generated CloudFormation template. Must be one of `json` or `yaml`.

`license-model`: (Optional, type _string_) - License model for the database.

`master-username`: (Required, type _string_) - The master username for the database.

`monitoring-interval`: (Optional, type _int_, default `0`) - The interval between points when enhanced monitoring is collected. A value of `0` disables enhanced monitoring.

`monitoring-role-arn`: (Optional, type _string_) - The ARN of the IAM role with permission to send enhanced monitoring to CloudWatch Logs.

`multi-az`: (Optional, type _bool_, default `false`) - Whether or not the database spans multiple availability zones.

`firewall`: (Optional, type _object_) - An object describing the firewall for the database (see [firewall](#firewall)).

`parameter-group`: (Optional, type _object_) - An object describing the parameter group or cluster parameter group (see [parameter-group](#parameter-group)).

`port`: (Required, type _int_) - The port number on which the database listens.

`preferred-backup-window`: (Optional, type _string_) - The window during which daily backups are taken. The format is `hh24:mi-hh24:mi` and must be a minimum of 30 minutes.

`preferred-maintenance-window`: (Optional, type _string_) - The window during which weekly maintenance is done. The format is `ddd:hh24:mi-ddd:hh24:mi` and must be a minimum of 30 minutes.

`replicas`: (Optional, type _array_ of _object_) - An array of objects describing replicas (see [replica](#replica)).

`replication-source-identifier`: (Optional, type _string_) - The ARN of the source instance or cluster if the cluster is being created as a read replica. Only valid when `engine` starts with `aurora`.

`scaling-configuration`: (Optional, type _object_) - An object describing the scaling configuration if `engine-mode` is `serverless` (see [scaling-configuration](#scaling-configuration)).

`storage`: (Optional, type _object_) - An object describing storage for the database (see [storage](#storage)).

`subnet-ids`: (Required, type _array_ of _string_) - The subnet IDs for the database.

`tags`: (Optional, type _object_ of _string_ -> _string_) - Tags to be added to the database.

`vpc-id`: (Required, type _string_) - The ID of the VPC for the database.

## dns

`domain`: (Required, type _string_) - The DNS domain to which hostnames should be added. A hosted zone for the domain must already be present in Route 53.

`reader-hostname`: (Conditional, type _string_) - The hostname of a reader. This is required if `engine` starts with `aurora` unless `engine-mode` is `serverless`. This is also required if `replicas` is nonempty, in which case a reader hostname will be defined for each replica with a numeric suffix appended. For example, if `reader-hostname` is `abc` and there are two replicas defined, then hostnames `abc-1` and `abc-2` will be created.

`ttl`: (Optional, type _int_, default `15`) - The TTL for DNS names.

`writer-hostname`: (Required, type _string_) - The hostname of a writer. This is the primary hostname for the database.

## firewall

`egress-rules`: (Optional, type _array_ of _object_) - An array of `egress-rule` objects (see [egress-rule](#egress-rule)).

`extra-security-groups`: (Optional, type _array_ of _string_) - An array of additional security group IDs to assign to the database.

`ingress-rules`: (Optional, type _array_ of _object_) - An array of `ingress-rule` objects (see [ingress-rule](#ingress-rule)).

## egress-rule

`cidr-ip`: (Optional, type _string_) - The destination IPv4 CIDR of the rule.

`cidr-ipv6`: (Optional, type _string_) - The destination IPv6 CIDR of the rule.

`destination-prefix-list-id`: (Optional, type _string_) - The destination prefix list ID of the rule.

`destination-security-group-id`: (Optional, type _string_) - The destination security group of the rule.

`from-port`: (Required, type _int_) - The starting port of the rule, must be a number between `0` and `65535`.

`ip-protocol`: (Required, type _string_) - The IP protocol of the rule. One of `tcp`, `udp`, `icmp`, `icmpv6`, or a protocol number between `-1` and `255`. Using `-1` specifies all protocols.

`to-port`: (Required, type _int_) - The ending port of the rule, must be a number between `0` and `65535`.

## ingress-rule

`cidr-ip`: (Optional, type _string_) - The source IPv4 CIDR of the rule.

`cidr-ipv6`: (Optional, type _string_) - The source IPv6 CIDR of the rule.

`source-security-group-id`: (Optional, type _string_) - The source security group of the rule.

`source-security-group-owner-id`: (Optional, type _string_) - The source security group owner ID of the rule.

`from-port`: (Required, type _int_) - The starting port of the rule, must be a number between `0` and `65535`.

`ip-protocol`: (Required, type _string_) - The IP protocol of the rule. One of `tcp`, `udp`, `icmp`, `icmpv6`, or a protocol number between `-1` and `255`. Using `-1` specifies all protocols.

`to-port`: (Required, type _int_) - The ending port of the rule, must be a number between `0` and `65535`.

## parameter-group

`family`: (Required, type _string_) - The name of the parameter group family. Use the command `aws rds describe-db-engine-versions --query "DBEngineVersions[].DBParameterGroupFamily"` to see available families.

`parameters`: (Optional, type _object_ of _string_ -> _string_) - Parameters for the database instance.

`cluster-parameters`: (Optional, type _object_ of _string_ -> _string_) - Parameters for the database cluster if using Aurora.

## replica

`instance-class`: (Required, type _string_) - The class of the instance.

`firewall`: (Required, type _object_) - An object describing the firewall for the replica (see [firewall](#firewall)).

`parameter-group`: (Optional, type _object_) - An object describing the parameter group (see [parameter-group](#parameter-group)).

`region`: (Optional, type _string_) - The region of the replica.

`tags`: (Optional, type _object_ of _string_ -> _string_) - Tags to be added to the replica.

## scaling-configuration

`auto-pause`: (Optional, type _bool_) - Whether or not to allow the cluster to automatically pause when idle.

`max-capacity`: (Optional, type _int_) - The maximum capacity of the cluster.

`min-capacity`: (Optional, type _int_) - The minimum capacity of the cluster.

`seconds-until-auto-pause`: (Optional, type _int_) - The number of seconds before the cluster is paused when idle.

## storage

`encrypted`: (Optional, type _bool_, default `true`) - Whether or not to encrypt the storage.

`iops`: (Conditional, type _int_) - The number of IOPs given to the storage. Required if `type` is `io1`.

`kms-key-id`: (Optional, type _string_) - The KMS key ID used to encrypt the storage.

`size`: (Conditional, type _int_) - The amount of storage in GiB. Required unless `engine` starts with `aurora`.

`type`: (Optional, type _string_) - The type of storage, which must be one of `standard`, `gp2`, and `io1`.

# Outputs

`template`: (type _string_) - The generated CloudFormation template.

# Example

```
imports: {
  cloudformation: 'github.com/cloudboss/unobin/modules/aws/cloudformation.CloudFormation'
  sqldb: 'github.com/cloudboss/unobin/modules/aws/sqldb.SqlDb'
}

task [generate sqldb template] {
  module: sqldb
  args: {
    database-name: 'concourse'
    dns: {
      domain: 'abc.example.com'
      reader-hostname: 'concourse-read'
      writer-hostname: 'concourse'
    }
    engine: 'aurora-postgresql'
    engine-version: '12.4'
    instance-class: 'db.r4.large'
    master-username: 'atc'
    firewall: {
      ingress-rules: [{
        cidr-ip: '10.62.64.0/20'
        from-port: 5432
        to-port: 5432
        ip-protocol: 'tcp'
      }]
    }
    port: 5432
    subnet-ids: [
      'subnet-0f9246d5bb46a52db'
      'subnet-071730ac75eed6a39'
    ]
    vpc-id: 'vpc-01a27fcbe2c474ace'
  }
}

task [ensure sqldb cluster stack is present] {
  module: cloudformation
  args: {
    stack-name: string-var.stack-name
    template-body: string-output[generate sqldb template].template
    parameters: { 'MasterUserPassword': string-var.password }
  }
}
```
