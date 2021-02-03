// Copyright © 2020 Joseph Wright <joseph@cloudboss.co>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package sqldb

import (
	"fmt"
	"strings"

	"github.com/awslabs/goformation/v4/cloudformation"
	"github.com/awslabs/goformation/v4/cloudformation/ec2"
	"github.com/awslabs/goformation/v4/cloudformation/rds"
	"github.com/awslabs/goformation/v4/cloudformation/route53"
	"github.com/awslabs/goformation/v4/cloudformation/tags"
	"github.com/cloudboss/unobin/pkg/aws"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
	"github.com/hashicorp/go-multierror"
	"github.com/mitchellh/mapstructure"
	"github.com/xeipuuv/gojsonschema"
)

const (
	moduleName               = "sqldb"
	aurora                   = "aurora"
	clusterKey               = "Cluster"
	clusterParameterGroupKey = "ClusterParameterGroup"
	cname                    = "CNAME"
	dnsRecordWriterKey       = "DnsRecordWriter"
	dnsRecordReaderKey       = "DnsRecordReader"
	masterInstanceKey        = "MasterInstance"
	masterParameterGroupKey  = "MasterParameterGroup"
	masterSecurityGroupKey   = "MasterSecurityGroup"
	masterUserPasswordKey    = "MasterUserPassword"
	subnetGroupKey           = "SubnetGroup"
)

type SqlDb struct {
	AllowMajorVersionUpgrade    bool
	AutoMinorVersionUpgrade     bool
	BackupRetentionPeriod       int64
	CopyTagsToSnapshot          bool
	DatabaseName                string
	InstanceClass               string
	Dns                         map[string]interface{}
	Engine                      string
	EngineMode                  string
	EngineVersion               string
	Format                      string
	LicenseModel                string
	MasterUsername              string
	MonitoringInterval          int64
	MonitoringRoleArn           string
	MultiAz                     bool
	Network                     map[string]interface{}
	ParameterGroup              map[string]interface{}
	Port                        int64
	PreferredBackupWindow       string
	PreferredMaintenanceWindow  string
	Replicas                    []interface{}
	ReplicationSourceIdentifier string
	ScalingConfiguration        map[string]interface{}
	SecurityGroup               map[string]interface{}
	StackName                   string
	Storage                     map[string]interface{}
	SubnetIds                   []interface{}
	Tags                        map[string]interface{}
	VpcId                       string
	sqldb                       *sqldb
	template                    *cloudformation.Template
	isAurora                    bool
	isServerless                bool
}

type sqldb struct {
	AllowMajorVersionUpgrade    *bool                 `mapstructure:"allow-major-version-upgrade"`
	AutoMinorVersionUpgrade     *bool                 `mapstructure:"auto-minor-version-upgrade"`
	BackupRetentionPeriod       *int64                `mapstructure:"backup-retention-period,omitempty"`
	CopyTagsToSnapshot          *bool                 `mapstructure:"copy-tags-to-snapshot"`
	DatabaseName                *string               `mapstructure:"database-name,omitempty"`
	InstanceClass               *string               `mapstructure:"instance-class"`
	Dns                         *dns                  `mapstructure:"dns,omitempty"`
	Engine                      *string               `mapstructure:"engine,omitempty"`
	EngineMode                  *string               `mapstructure:"engine-mode,omitempty"`
	EngineVersion               *string               `mapstructure:"engine-version,omitempty"`
	MasterUsername              *string               `mapstructure:"master-username,omitempty"`
	MonitoringInterval          *int64                `mapstructure:"monitoring-interval"`
	MonitoringRoleArn           *string               `mapstructure:"monitoring-role-arn"`
	MultiAz                     *bool                 `mapstructure:"multi-az"`
	Network                     *network              `mapstructure:"network,omitempty"`
	ParameterGroup              *parameterGroup       `mapstructure:"parameter-group,omitempty"`
	Port                        *int64                `mapstructure:"port,omitempty"`
	PreferredBackupWindow       *string               `mapstructure:"preferred-backup-window,omitempty"`
	PreferredMaintenanceWindow  *string               `mapstructure:"preferred-maintenance-window,omitempty"`
	Replicas                    []replica             `mapstructure:"replicas"`
	ReplicationSourceIdentifier *string               `mapstructure:"replication-source-identifier,omitempty"`
	ScalingConfiguration        *scalingConfiguration `mapstructure:"scaling-configuration,omitempty"`
	Storage                     *storage              `mapstructure:"storage,omitempty"`
	SubnetIds                   []string              `mapstructure:"subnet-ids"`
	Tags                        map[string]string     `mapstructure:"tags,omitempty"`
	VpcId                       *string               `mapstructure:"vpc-id"`
}

type dns struct {
	Domain         *string `mapstructure:"domain,omitempty"`
	ReaderHostname *string `mapstructure:"reader-hostname,omitempty"`
	Ttl            *int64  `mapstructure:"ttl,omitempty"`
	WriterHostname *string `mapstructure:"writer-hostname,omitempty"`
}

type network struct {
	EgressRules         []egressRule  `mapstructure:"egress-rules,omitempty"`
	ExtraSecurityGroups []string      `mapstructure:"extra-security-groups,omitempty"`
	IngressRules        []ingressRule `mapstructure:"ingress-rules,omitempty"`
}

type parameterGroup struct {
	Family            *string           `mapstructure:"family,omitempty"`
	Parameters        map[string]string `mapstructure:"parameters,omitempty"`
	ClusterParameters map[string]string `mapstructure:"cluster-parameters,omitempty"`
}

type replica struct {
	InstanceClass  *string           `mapstructure:"instance-class"`
	Network        *network          `mapstructure:"network"`
	ParameterGroup *parameterGroup   `mapstructure:"parameter-group,omitempty"`
	Region         *string           `mapstructure:"region,omitempty"`
	Tags           map[string]string `mapstructure:"tags,omitempty"`
}

type scalingConfiguration struct {
	AutoPause             *bool  `mapstructure:"auto-pause,omitempty"`
	MaxCapacity           *int64 `mapstructure:"max-capacity,omitempty"`
	MinCapacity           *int64 `mapstructure:"min-capacity,omitempty"`
	SecondsUntilAutoPause *int64 `mapstructure:"seconds-until-auto-pause,omitempty"`
}

type ingressRule struct {
	FromPort                   *int64  `mapstructure:"from-port,omitempty"`
	ToPort                     *int64  `mapstructure:"to-port,omitempty"`
	IpProtocol                 *string `mapstructure:"ip-protocol,omitempty"`
	CidrIp                     *string `mapstructure:"cidr-ip,omitempty"`
	CidrIpv6                   *string `mapstructure:"cidr-ipv6,omitempty"`
	SourceSecurityGroupId      *string `mapstructure:"source-security-group-id,omitempty"`
	SourceSecurityGroupOwnerId *string `mapstructure:"source-security-group-owner-id,omitempty"`
}

type egressRule struct {
	FromPort                   *int64  `mapstructure:"from-port,omitempty"`
	ToPort                     *int64  `mapstructure:"to-port,omitempty"`
	IpProtocol                 *string `mapstructure:"ip-protocol,omitempty"`
	CidrIp                     *string `mapstructure:"cidr-ip,omitempty"`
	CidrIpv6                   *string `mapstructure:"cidr-ipv6,omitempty"`
	DestinationPrefixListId    *string `mapstructure:"destination-prefix-list-id,omitempty"`
	DestinationSecurityGroupId *string `mapstructure:"destination-security-group-id,omitempty"`
}

type storage struct {
	Encrypted *bool   `mapstructure:"encrypted,omitempty"`
	Iops      *int64  `mapstructure:"iops,omitempty"`
	KmsKeyId  *string `mapstructure:"kms-key-id,omitempty"`
	Size      *int64  `mapstructure:"size,omitempty"`
	Type      *string `mapstructure:"type,omitempty"`
}

func (s *SqlDb) Initialize() error {
	args := map[string]interface{}{
		"allow-major-version-upgrade": s.AllowMajorVersionUpgrade,
		"auto-minor-version-upgrade":  s.AutoMinorVersionUpgrade,
		"backup-retention-period":     s.BackupRetentionPeriod,
		"copy-tags-to-snapshot":       s.CopyTagsToSnapshot,
		"multi-az":                    s.MultiAz,
		"monitoring-interval":         s.MonitoringInterval,
	}
	if s.DatabaseName != "" {
		args["database-name"] = s.DatabaseName
	}
	if s.Dns != nil {
		args["dns"] = s.Dns
	}
	if s.Engine != "" {
		args["engine"] = s.Engine
	}
	if s.EngineMode != "" {
		args["engine-mode"] = s.EngineMode
	}
	if s.EngineVersion != "" {
		args["engine-version"] = s.EngineVersion
	}
	if s.InstanceClass != "" {
		args["instance-class"] = s.InstanceClass
	}
	if s.LicenseModel != "" {
		args["license-model"] = s.LicenseModel
	}
	if s.MasterUsername != "" {
		args["master-username"] = s.MasterUsername
	}
	if s.MonitoringRoleArn != "" {
		args["monitoring-role-arn"] = s.MonitoringRoleArn
	}
	if s.Network != nil {
		args["network"] = s.Network
	}
	if s.ParameterGroup != nil {
		args["parameter-group"] = s.ParameterGroup
	}
	if s.Port != 0 {
		args["port"] = s.Port
	}
	if s.PreferredBackupWindow != "" {
		args["preferred-backup-window"] = s.PreferredBackupWindow
	}
	if s.PreferredMaintenanceWindow != "" {
		args["preferred-maintenance-window"] = s.PreferredMaintenanceWindow
	}
	if s.Replicas != nil {
		args["replicas"] = s.Replicas
	}
	if s.ReplicationSourceIdentifier != "" {
		args["replication-source-identifier"] = s.ReplicationSourceIdentifier
	}
	if s.ScalingConfiguration != nil {
		args["scaling-configuration"] = s.ScalingConfiguration
	}
	if s.Storage != nil {
		args["storage"] = s.Storage
	}
	if s.SubnetIds != nil {
		args["subnet-ids"] = s.SubnetIds
	}
	if s.VpcId != "" {
		args["vpc-id"] = s.VpcId
	}

	err := validate(args)
	if err != nil {
		return err
	}

	sqldb, err := decode(args)
	if err != nil {
		return err
	}

	s.isAurora = strings.HasPrefix(s.Engine, aurora)
	s.isServerless = s.EngineMode == "serverless"
	s.sqldb = sqldb
	s.template = cloudformation.NewTemplate()

	return nil
}

func (s *SqlDb) Name() string {
	return moduleName
}

func (s *SqlDb) Apply() *types.Result {
	if s.isAurora {
		s.defineTemplateAuroraCluster()
		s.defineTemplateAuroraDns()
		if !s.isServerless {
			s.defineTemplateAuroraMasterInstance()
			s.defineTemplateAuroraReplicas()
		}
	} else {
		s.defineTemplateMasterInstance()
		s.defineTemplateReplicas()
		s.defineTemplateDns()
	}
	s.defineTemplateCommonResources()
	s.defineTemplateParameters()
	s.defineTemplateOutputs()

	template, err := aws.GenerateCloudFormationTemplate(s.Format, s.template)
	if err != nil {
		return util.ResultFailedUnchanged(moduleName, err.Error())
	}

	return &types.Result{
		Succeeded: true,
		Changed:   true,
		Module:    moduleName,
		Output:    map[string]interface{}{"template": template},
	}
}

func (s *SqlDb) Destroy() *types.Result {
	return nil
}

func (s *SqlDb) defineTemplateAuroraCluster() {
	clusterRsc := &rds.DBCluster{
		DatabaseName:        *s.sqldb.DatabaseName,
		DBClusterIdentifier: s.StackName,
		DBSubnetGroupName:   cloudformation.Ref(subnetGroupKey),
		Engine:              *s.sqldb.Engine,
		EngineVersion:       *s.sqldb.EngineVersion,
		MasterUsername:      *s.sqldb.MasterUsername,
		MasterUserPassword:  cloudformation.Ref(masterUserPasswordKey),
		Port:                int(*s.sqldb.Port),
		StorageEncrypted:    *s.sqldb.Storage.Encrypted,
	}
	if s.sqldb.BackupRetentionPeriod != nil {
		clusterRsc.BackupRetentionPeriod = int(*s.sqldb.BackupRetentionPeriod)
	}
	if s.sqldb.EngineMode != nil {
		clusterRsc.EngineMode = *s.sqldb.EngineMode
	}
	if s.sqldb.ParameterGroup != nil && s.sqldb.ParameterGroup.ClusterParameters != nil {
		clusterRsc.DBClusterParameterGroupName = cloudformation.Ref(clusterParameterGroupKey)
	}
	if s.sqldb.PreferredBackupWindow != nil {
		clusterRsc.PreferredBackupWindow = *s.sqldb.PreferredBackupWindow
	}
	if s.sqldb.PreferredMaintenanceWindow != nil {
		clusterRsc.PreferredMaintenanceWindow = *s.sqldb.PreferredMaintenanceWindow
	}
	if s.sqldb.ReplicationSourceIdentifier != nil {
		clusterRsc.ReplicationSourceIdentifier = *s.sqldb.ReplicationSourceIdentifier
	}
	if s.sqldb.ScalingConfiguration != nil {
		clusterRsc.ScalingConfiguration = &rds.DBCluster_ScalingConfiguration{}
		if s.sqldb.ScalingConfiguration.AutoPause != nil {
			clusterRsc.ScalingConfiguration.AutoPause = *s.sqldb.ScalingConfiguration.AutoPause
		}
		if s.sqldb.ScalingConfiguration.MaxCapacity != nil {
			clusterRsc.ScalingConfiguration.MaxCapacity = int(*s.sqldb.ScalingConfiguration.MaxCapacity)
		}
		if s.sqldb.ScalingConfiguration.MinCapacity != nil {
			clusterRsc.ScalingConfiguration.MinCapacity = int(*s.sqldb.ScalingConfiguration.MinCapacity)
		}
		if s.sqldb.ScalingConfiguration.SecondsUntilAutoPause != nil {
			clusterRsc.ScalingConfiguration.SecondsUntilAutoPause = int(*s.sqldb.ScalingConfiguration.SecondsUntilAutoPause)
		}
	}
	if len(s.sqldb.Tags) > 0 {
		clusterRsc.Tags = make([]tags.Tag, len(s.sqldb.Tags))
		i := 0
		for k, v := range s.sqldb.Tags {
			clusterRsc.Tags[i] = tags.Tag{Key: k, Value: v}
			i++
		}
	}
	clusterRsc.VpcSecurityGroupIds = make([]string, len(s.sqldb.Network.ExtraSecurityGroups)+1)
	clusterRsc.VpcSecurityGroupIds[0] = cloudformation.Ref(masterSecurityGroupKey)
	for i := range s.sqldb.Network.ExtraSecurityGroups {
		clusterRsc.VpcSecurityGroupIds[i+1] = s.sqldb.Network.ExtraSecurityGroups[i]
	}
	s.template.Resources[clusterKey] = clusterRsc
}

func (s *SqlDb) defineTemplateAuroraMasterInstance() {
	masterInstanceRsc := &rds.DBInstance{
		AllowMajorVersionUpgrade: *s.sqldb.AllowMajorVersionUpgrade,
		AutoMinorVersionUpgrade:  *s.sqldb.AutoMinorVersionUpgrade,
		CopyTagsToSnapshot:       *s.sqldb.CopyTagsToSnapshot,
		DBClusterIdentifier:      cloudformation.Ref(clusterKey),
		DBInstanceClass:          *s.sqldb.InstanceClass,
		DBInstanceIdentifier:     s.StackName,
		DBSubnetGroupName:        cloudformation.Ref(subnetGroupKey),
		Engine:                   *s.sqldb.Engine,
		EngineVersion:            *s.sqldb.EngineVersion,
		MonitoringInterval:       int(*s.sqldb.MonitoringInterval),
		MultiAZ:                  *s.sqldb.MultiAz,
	}
	if s.sqldb.ParameterGroup != nil && s.sqldb.ParameterGroup.Parameters != nil {
		masterInstanceRsc.DBParameterGroupName = cloudformation.Ref(masterParameterGroupKey)
	}
	if s.sqldb.MonitoringRoleArn != nil {
		masterInstanceRsc.MonitoringRoleArn = *s.sqldb.MonitoringRoleArn
	}
	if len(s.sqldb.Tags) > 0 {
		masterInstanceRsc.Tags = make([]tags.Tag, len(s.sqldb.Tags))
		i := 0
		for k, v := range s.sqldb.Tags {
			masterInstanceRsc.Tags[i] = tags.Tag{Key: k, Value: v}
			i++
		}
	}
	s.template.Resources[masterInstanceKey] = masterInstanceRsc
}

func (s *SqlDb) defineTemplateMasterInstance() {
	masterInstanceRsc := &rds.DBInstance{
		AllocatedStorage:         fmt.Sprint(*s.sqldb.Storage.Size),
		AllowMajorVersionUpgrade: *s.sqldb.AllowMajorVersionUpgrade,
		AutoMinorVersionUpgrade:  *s.sqldb.AutoMinorVersionUpgrade,
		CopyTagsToSnapshot:       *s.sqldb.CopyTagsToSnapshot,
		DBInstanceClass:          *s.sqldb.InstanceClass,
		DBInstanceIdentifier:     s.StackName,
		DBName:                   *s.sqldb.DatabaseName,
		DBSubnetGroupName:        cloudformation.Ref(subnetGroupKey),
		Engine:                   *s.sqldb.Engine,
		EngineVersion:            *s.sqldb.EngineVersion,
		MasterUsername:           *s.sqldb.MasterUsername,
		MasterUserPassword:       cloudformation.Ref(masterUserPasswordKey),
		MonitoringInterval:       int(*s.sqldb.MonitoringInterval),
		MultiAZ:                  *s.sqldb.MultiAz,
		Port:                     fmt.Sprint(*s.sqldb.Port),
		StorageEncrypted:         *s.sqldb.Storage.Encrypted,
		StorageType:              *s.sqldb.Storage.Type,
	}
	if s.sqldb.BackupRetentionPeriod != nil {
		masterInstanceRsc.BackupRetentionPeriod = int(*s.sqldb.BackupRetentionPeriod)
	}
	if s.sqldb.ParameterGroup != nil && s.sqldb.ParameterGroup.Parameters != nil {
		masterInstanceRsc.DBParameterGroupName = cloudformation.Ref(masterParameterGroupKey)
	}
	if s.sqldb.Storage.Iops != nil {
		masterInstanceRsc.Iops = int(*s.sqldb.Storage.Iops)
	}
	if s.sqldb.Storage.KmsKeyId != nil {
		masterInstanceRsc.KmsKeyId = *s.sqldb.Storage.KmsKeyId
	}
	if s.LicenseModel != "" {
		masterInstanceRsc.LicenseModel = s.LicenseModel
	}
	if s.sqldb.MonitoringRoleArn != nil {
		masterInstanceRsc.MonitoringRoleArn = *s.sqldb.MonitoringRoleArn
	}
	if len(s.sqldb.Tags) > 0 {
		masterInstanceRsc.Tags = make([]tags.Tag, len(s.sqldb.Tags))
		i := 0
		for k, v := range s.sqldb.Tags {
			masterInstanceRsc.Tags[i] = tags.Tag{Key: k, Value: v}
			i++
		}
	}
	masterInstanceRsc.VPCSecurityGroups = make([]string, len(s.sqldb.Network.ExtraSecurityGroups)+1)
	masterInstanceRsc.VPCSecurityGroups[0] = cloudformation.Ref(masterSecurityGroupKey)
	for i := range s.sqldb.Network.ExtraSecurityGroups {
		masterInstanceRsc.VPCSecurityGroups[i+1] = s.sqldb.Network.ExtraSecurityGroups[i]
	}
	s.template.Resources[masterInstanceKey] = masterInstanceRsc
}

func (s *SqlDb) defineTemplateAuroraReplicas() {
	for i, replica := range s.sqldb.Replicas {
		replicaInstanceKey := fmt.Sprintf("Replica%dInstance", i)
		replicaInstanceRsc := &rds.DBInstance{
			DBClusterIdentifier:  cloudformation.Ref(clusterKey),
			DBInstanceIdentifier: fmt.Sprintf("%s-replica-%d", s.StackName, i),
			DBInstanceClass:      *replica.InstanceClass,
			// DBSubnetGroupName:    cloudformation.Ref(subnetGroupKey),
			Engine:             *s.sqldb.Engine,
			EngineVersion:      *s.sqldb.EngineVersion,
			MonitoringInterval: int(*s.sqldb.MonitoringInterval),
			MultiAZ:            *s.sqldb.MultiAz,
		}
		replicaInstanceRsc.AWSCloudFormationDependsOn = []string{masterInstanceKey}
		s.template.Resources[replicaInstanceKey] = replicaInstanceRsc
		if replica.ParameterGroup != nil && replica.ParameterGroup.Parameters != nil {
			replicaParameterGroupKey := fmt.Sprintf("Replica%dParameterGroup", i)
			replicaInstanceRsc.DBParameterGroupName = cloudformation.Ref(replicaParameterGroupKey)
		}
		if s.sqldb.MonitoringRoleArn != nil {
			replicaInstanceRsc.MonitoringRoleArn = *s.sqldb.MonitoringRoleArn
		}
		if len(replica.Tags) > 0 {
			replicaInstanceRsc.Tags = make([]tags.Tag, len(replica.Tags))
			i := 0
			for k, v := range replica.Tags {
				replicaInstanceRsc.Tags[i] = tags.Tag{Key: k, Value: v}
				i++
			}
		}
	}
}

func (s *SqlDb) defineTemplateReplicas() {
	for i, replica := range s.sqldb.Replicas {
		replicaInstanceKey := fmt.Sprintf("Replica%dInstance", i)
		replicaInstanceName := fmt.Sprintf("%s-replica-%d", s.StackName, i)
		replicaInstanceRsc := &rds.DBInstance{
			DBInstanceIdentifier:       replicaInstanceName,
			DBInstanceClass:            *replica.InstanceClass,
			MonitoringInterval:         int(*s.sqldb.MonitoringInterval),
			MultiAZ:                    *s.sqldb.MultiAz,
			SourceDBInstanceIdentifier: cloudformation.Ref(masterInstanceKey),
		}
		s.template.Resources[replicaInstanceKey] = replicaInstanceRsc
		if replica.ParameterGroup != nil && replica.ParameterGroup.Parameters != nil {
			replicaParameterGroupKey := fmt.Sprintf("Replica%dParameterGroup", i)
			replicaInstanceRsc.DBParameterGroupName = cloudformation.Ref(replicaParameterGroupKey)
		}
		if s.sqldb.MonitoringRoleArn != nil {
			replicaInstanceRsc.MonitoringRoleArn = *s.sqldb.MonitoringRoleArn
		}
		if replica.Region != nil {
			replicaInstanceRsc.SourceRegion = *replica.Region
		}
		if len(replica.Tags) > 0 {
			replicaInstanceRsc.Tags = make([]tags.Tag, len(replica.Tags))
			i := 0
			for k, v := range replica.Tags {
				replicaInstanceRsc.Tags[i] = tags.Tag{Key: k, Value: v}
				i++
			}
		}
		replicaInstanceRsc.VPCSecurityGroups = make([]string, len(replica.Network.ExtraSecurityGroups)+1)
		replicaInstanceRsc.VPCSecurityGroups[0] = cloudformation.Ref(fmt.Sprintf("Replica%dSecurityGroup", i))
		for i := range replica.Network.ExtraSecurityGroups {
			replicaInstanceRsc.VPCSecurityGroups[i+1] = replica.Network.ExtraSecurityGroups[i]
		}
		replicaSecurityGroupKey := fmt.Sprintf("Replica%dSecurityGroup", i)
		replicaSecurityGroupRsc := &ec2.SecurityGroup{
			GroupDescription: fmt.Sprintf("Security group for %s", replicaInstanceName),
			VpcId:            *s.sqldb.VpcId,
		}
		s.template.Resources[replicaSecurityGroupKey] = replicaSecurityGroupRsc
		for j, ingress := range replica.Network.IngressRules {
			ingressRuleKey := fmt.Sprintf("Replica%dSecurityGroupIngress%d", i, j)
			ingressRuleRsc := &ec2.SecurityGroupIngress{
				FromPort:   int(*ingress.FromPort),
				GroupId:    cloudformation.Ref(replicaSecurityGroupKey),
				IpProtocol: *ingress.IpProtocol,
				ToPort:     int(*ingress.ToPort),
			}
			if ingress.CidrIp != nil {
				ingressRuleRsc.CidrIp = *ingress.CidrIp
			}
			if ingress.CidrIpv6 != nil {
				ingressRuleRsc.CidrIpv6 = *ingress.CidrIpv6
			}
			if ingress.SourceSecurityGroupId != nil {
				ingressRuleRsc.SourceSecurityGroupId = *ingress.SourceSecurityGroupId
			}
			s.template.Resources[ingressRuleKey] = ingressRuleRsc
		}
		for j, egress := range replica.Network.EgressRules {
			egressRuleKey := fmt.Sprintf("Replica%dSecurityGroupEgress%d", i, j)
			egressRuleRsc := &ec2.SecurityGroupEgress{
				FromPort:   int(*egress.FromPort),
				GroupId:    cloudformation.Ref(replicaSecurityGroupKey),
				IpProtocol: *egress.IpProtocol,
				ToPort:     int(*egress.ToPort),
			}
			if egress.CidrIp != nil {
				egressRuleRsc.CidrIp = *egress.CidrIp
			}
			if egress.CidrIpv6 != nil {
				egressRuleRsc.CidrIpv6 = *egress.CidrIpv6
			}
			if egress.DestinationPrefixListId != nil {
				egressRuleRsc.DestinationPrefixListId = *egress.DestinationPrefixListId
			}
			if egress.DestinationSecurityGroupId != nil {
				egressRuleRsc.DestinationSecurityGroupId = *egress.DestinationSecurityGroupId
			}
			s.template.Resources[egressRuleKey] = egressRuleRsc
		}
	}
}

func (s *SqlDb) defineTemplateAuroraDns() {
	if s.sqldb.Dns == nil {
		return
	}
	if !s.isServerless {
		dnsRecordReaderRsc := &route53.RecordSet{
			HostedZoneName:  fmt.Sprintf("%s.", *s.sqldb.Dns.Domain),
			Name:            fmt.Sprintf("%s.%s.", *s.sqldb.Dns.ReaderHostname, *s.sqldb.Dns.Domain),
			ResourceRecords: []string{cloudformation.GetAtt(clusterKey, "ReadEndpoint.Address")},
			Type:            cname,
			TTL:             fmt.Sprint(*s.sqldb.Dns.Ttl),
		}
		s.template.Resources[dnsRecordReaderKey] = dnsRecordReaderRsc
	}
	dnsRecordWriterRsc := &route53.RecordSet{
		HostedZoneName:  fmt.Sprintf("%s.", *s.sqldb.Dns.Domain),
		Name:            fmt.Sprintf("%s.%s.", *s.sqldb.Dns.WriterHostname, *s.sqldb.Dns.Domain),
		ResourceRecords: []string{cloudformation.GetAtt(clusterKey, "Endpoint.Address")},
		Type:            cname,
		TTL:             fmt.Sprint(*s.sqldb.Dns.Ttl),
	}
	s.template.Resources[dnsRecordWriterKey] = dnsRecordWriterRsc
}

func (s *SqlDb) defineTemplateDns() {
	if s.sqldb.Dns == nil {
		return
	}
	for i := range s.sqldb.Replicas {
		dnsRecordReaderReplicaKey := fmt.Sprintf("%s%d", dnsRecordReaderKey, i)
		replicaInstanceKey := fmt.Sprintf("Replica%dInstance", i)
		dnsRecordReaderRsc := &route53.RecordSet{
			HostedZoneName:  fmt.Sprintf("%s.", *s.sqldb.Dns.Domain),
			Name:            fmt.Sprintf("%s-%d.%s", *s.sqldb.Dns.ReaderHostname, i, *s.sqldb.Dns.Domain),
			ResourceRecords: []string{cloudformation.GetAtt(replicaInstanceKey, "Endpoint.Address")},
			Type:            cname,
			TTL:             fmt.Sprint(*s.sqldb.Dns.Ttl),
		}
		s.template.Resources[dnsRecordReaderReplicaKey] = dnsRecordReaderRsc
	}
	dnsRecordWriterRsc := &route53.RecordSet{
		HostedZoneName:  fmt.Sprintf("%s.", *s.sqldb.Dns.Domain),
		Name:            fmt.Sprintf("%s.%s.", *s.sqldb.Dns.WriterHostname, *s.sqldb.Dns.Domain),
		ResourceRecords: []string{cloudformation.GetAtt(masterInstanceKey, "Endpoint.Address")},
		Type:            cname,
		TTL:             fmt.Sprint(*s.sqldb.Dns.Ttl),
	}
	s.template.Resources[dnsRecordWriterKey] = dnsRecordWriterRsc
}

func (s *SqlDb) defineTemplateCommonResources() {
	s.template.Resources[subnetGroupKey] = &rds.DBSubnetGroup{
		DBSubnetGroupName:        s.StackName,
		DBSubnetGroupDescription: s.StackName,
		SubnetIds:                s.sqldb.SubnetIds,
	}
	s.template.Resources[masterSecurityGroupKey] = &ec2.SecurityGroup{
		GroupDescription: fmt.Sprintf("Security group for %s", s.StackName),
		VpcId:            *s.sqldb.VpcId,
	}
	for i, ingress := range s.sqldb.Network.IngressRules {
		ingressKey := fmt.Sprintf("%sIngress%d", masterSecurityGroupKey, i)
		ingressRsc := &ec2.SecurityGroupIngress{
			FromPort:   int(*ingress.FromPort),
			GroupId:    cloudformation.Ref(masterSecurityGroupKey),
			IpProtocol: *ingress.IpProtocol,
			ToPort:     int(*ingress.ToPort),
		}
		if ingress.CidrIp != nil {
			ingressRsc.CidrIp = *ingress.CidrIp
		}
		if ingress.CidrIpv6 != nil {
			ingressRsc.CidrIpv6 = *ingress.CidrIpv6
		}
		if ingress.SourceSecurityGroupId != nil {
			ingressRsc.SourceSecurityGroupId = *ingress.SourceSecurityGroupId
		}
		s.template.Resources[ingressKey] = ingressRsc
	}
	for i, egress := range s.sqldb.Network.EgressRules {
		egressKey := fmt.Sprintf("%sEgress%d", masterSecurityGroupKey, i)
		egressRsc := &ec2.SecurityGroupEgress{
			FromPort:   int(*egress.FromPort),
			GroupId:    cloudformation.Ref(masterSecurityGroupKey),
			IpProtocol: *egress.IpProtocol,
			ToPort:     int(*egress.ToPort),
		}
		if egress.CidrIp != nil {
			egressRsc.CidrIp = *egress.CidrIp
		}
		if egress.CidrIpv6 != nil {
			egressRsc.CidrIpv6 = *egress.CidrIpv6
		}
		if egress.DestinationPrefixListId != nil {
			egressRsc.DestinationPrefixListId = *egress.DestinationPrefixListId
		}
		if egress.DestinationSecurityGroupId != nil {
			egressRsc.DestinationSecurityGroupId = *egress.DestinationSecurityGroupId
		}
		s.template.Resources[egressKey] = egressRsc
	}
	if s.sqldb.ParameterGroup != nil && s.sqldb.ParameterGroup.Parameters != nil {
		s.template.Resources[masterParameterGroupKey] = &rds.DBParameterGroup{
			Description: s.StackName,
			Family:      *s.sqldb.ParameterGroup.Family,
			Parameters:  s.sqldb.ParameterGroup.Parameters,
		}
	}
	if s.sqldb.ParameterGroup != nil && s.sqldb.ParameterGroup.ClusterParameters != nil {
		s.template.Resources[clusterParameterGroupKey] = &rds.DBClusterParameterGroup{
			Description: s.StackName,
			Family:      *s.sqldb.ParameterGroup.Family,
			Parameters:  s.sqldb.ParameterGroup.Parameters,
		}
	}
}

func (s *SqlDb) defineTemplateParameters() {
	s.template.Parameters = cloudformation.Parameters{
		masterUserPasswordKey: cloudformation.Parameter{
			Description: "Password of master database user",
			NoEcho:      true,
			Type:        "String",
		},
	}
}

func (s *SqlDb) defineTemplateOutputs() {
	amazonEndpointKey := "AmazonEndpoint"
	amazonReadEndpointKey := "AmazonReadEndpointAddress"
	endpointPortKey := "EndpointPort"
	if s.isAurora {
		s.template.Outputs[amazonEndpointKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: amazonEndpointKey},
			Value:  cloudformation.GetAtt(clusterKey, "Endpoint.Address"),
		}
		s.template.Outputs[endpointPortKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: endpointPortKey},
			Value:  cloudformation.GetAtt(clusterKey, "Endpoint.Port"),
		}
		if !s.isServerless {
			s.template.Outputs[amazonReadEndpointKey] = cloudformation.Output{
				Export: cloudformation.Export{Name: amazonReadEndpointKey},
				Value:  cloudformation.GetAtt(clusterKey, "ReadEndpoint.Address"),
			}
		}
	} else {
		s.template.Outputs[amazonEndpointKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: amazonEndpointKey},
			Value:  cloudformation.GetAtt(masterInstanceKey, "Endpoint.Address"),
		}
		s.template.Outputs[endpointPortKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: endpointPortKey},
			Value:  cloudformation.GetAtt(masterInstanceKey, "Endpoint.Port"),
		}
	}
	if s.sqldb.Dns != nil {
		domainEndpointKey := "DomainEndpoint"
		s.template.Outputs[domainEndpointKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: domainEndpointKey},
			Value:  fmt.Sprintf("%s.%s", *s.sqldb.Dns.WriterHostname, *s.sqldb.Dns.Domain),
		}
		if !s.isServerless {
			domainReadEndpointKey := "DomainReadEndpointAddress"
			s.template.Outputs[domainReadEndpointKey] = cloudformation.Output{
				Export: cloudformation.Export{Name: domainReadEndpointKey},
				Value:  fmt.Sprintf("%s.%s", *s.sqldb.Dns.ReaderHostname, *s.sqldb.Dns.Domain),
			}
		}
	}
	s.template.Outputs[masterSecurityGroupKey] = cloudformation.Output{
		Export: cloudformation.Export{Name: masterSecurityGroupKey},
		Value:  cloudformation.Ref(masterSecurityGroupKey),
	}
	if s.isServerless {
		return
	}
	masterInstanceKey := "MasterInstance"
	s.template.Outputs[masterInstanceKey] = cloudformation.Output{
		Export: cloudformation.Export{Name: masterInstanceKey},
		Value:  cloudformation.Ref(masterInstanceKey),
	}
	masterInstanceAddressKey := "MasterInstanceAddress"
	s.template.Outputs[masterInstanceAddressKey] = cloudformation.Output{
		Export: cloudformation.Export{Name: masterInstanceAddressKey},
		Value:  cloudformation.GetAtt(masterInstanceKey, "Endpoint.Address"),
	}
	masterInstancePortKey := "MasterInstancePort"
	s.template.Outputs[masterInstancePortKey] = cloudformation.Output{
		Export: cloudformation.Export{Name: masterInstancePortKey},
		Value:  cloudformation.GetAtt(masterInstanceKey, "Endpoint.Port"),
	}
	for i := range s.sqldb.Replicas {
		if !s.isAurora {
			replicaSecurityGroupKey := fmt.Sprintf("Replica%dSecurityGroup", i)
			s.template.Outputs[replicaSecurityGroupKey] = cloudformation.Output{
				Export: cloudformation.Export{Name: replicaSecurityGroupKey},
				Value:  cloudformation.Ref(replicaSecurityGroupKey),
			}
		}
		replicaInstanceKey := fmt.Sprintf("Replica%dInstance", i)
		s.template.Outputs[replicaInstanceKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: replicaInstanceKey},
			Value:  cloudformation.Ref(replicaInstanceKey),
		}
		replicaInstanceAddressKey := fmt.Sprintf("Replica%dInstanceAddress", i)
		s.template.Outputs[replicaInstanceAddressKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: replicaInstanceAddressKey},
			Value:  cloudformation.GetAtt(replicaInstanceKey, "Endpoint.Address"),
		}
		replicaInstancePortKey := fmt.Sprintf("Replica%dInstancePort", i)
		s.template.Outputs[replicaInstancePortKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: replicaInstancePortKey},
			Value:  cloudformation.GetAtt(replicaInstanceKey, "Endpoint.Port"),
		}
	}
}

func validate(args map[string]interface{}) error {
	schema := map[string]interface{}{
		"additionalProperties": false,
		"type":                 "object",
		"properties": map[string]interface{}{
			"allow-major-version-upgrade":   map[string]interface{}{"type": "boolean"},
			"auto-minor-version-upgrade":    map[string]interface{}{"type": "boolean"},
			"backup-retention-period":       map[string]interface{}{"type": "integer"},
			"copy-tags-to-snapshot":         map[string]interface{}{"type": "boolean"},
			"database-name":                 map[string]interface{}{"$ref": "#/definitions/non-empty-string"},
			"instance-class":                map[string]interface{}{"type": "string"},
			"dns":                           map[string]interface{}{"$ref": "#/definitions/dns"},
			"engine":                        map[string]interface{}{"type": "string"},
			"engine-mode":                   map[string]interface{}{"type": "string"},
			"engine-version":                map[string]interface{}{"type": "string"},
			"master-username":               map[string]interface{}{"type": "string"},
			"monitoring-interval":           map[string]interface{}{"type": "integer"},
			"monitoring-role-arn":           map[string]interface{}{"type": "string"},
			"multi-az":                      map[string]interface{}{"type": "boolean"},
			"network":                       map[string]interface{}{"$ref": "#/definitions/network"},
			"parameter-group":               map[string]interface{}{"$ref": "#/definitions/parameter-group"},
			"port":                          map[string]interface{}{"$ref": "#/definitions/valid-port"},
			"preferred-backup-window":       map[string]interface{}{"type": "string"},
			"preferred-maintenance-window":  map[string]interface{}{"type": "string"},
			"replicas":                      map[string]interface{}{"type": []interface{}{"array", "null"}},
			"replication-source-identifier": map[string]interface{}{"type": "string"},
			"scaling-configuration":         map[string]interface{}{"$ref": "#/definitions/scaling-configuration"},
			"storage":                       map[string]interface{}{"$ref": "#/definitions/storage"},
			"subnet-ids":                    map[string]interface{}{"type": "array", "minItems": 2},
			"tags":                          map[string]interface{}{"type": []interface{}{"object", "null"}},
			"vpc-id":                        map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{
			"database-name", "engine", "engine-version", "instance-class",
			"master-username", "network", "port", "subnet-ids", "vpc-id",
		},
		"definitions": map[string]interface{}{
			"dns": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domain":          map[string]interface{}{"type": "string"},
					"reader-hostname": map[string]interface{}{"type": "string"},
					"ttl":             map[string]interface{}{"type": "integer"},
					"writer-hostname": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"domain"},
			},
			"egress-rule": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cidr-ip":                       map[string]interface{}{"type": "string"},
					"cidr-ipv6":                     map[string]interface{}{"type": "string"},
					"destination-prefix-list-id":    map[string]interface{}{"type": "string"},
					"destination-security-group-id": map[string]interface{}{"type": "string"},
					"from-port":                     map[string]interface{}{"$ref": "#/definitions/valid-port"},
					"ip-protocol":                   map[string]interface{}{"type": "string"},
					"to-port":                       map[string]interface{}{"$ref": "#/definitions/valid-port"},
				},
				"oneOf": []interface{}{
					map[string]interface{}{"required": []interface{}{"cidr-ip"}},
					map[string]interface{}{"required": []interface{}{"cidr-ipv6"}},
					map[string]interface{}{"required": []interface{}{"destination-prefix-list-id"}},
					map[string]interface{}{"required": []interface{}{"destination-security-group-id"}},
				},
				"required": []interface{}{"from-port", "to-port"},
			},
			"ingress-rule": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cidr-ip":                        map[string]interface{}{"type": "string"},
					"cidr-ipv6":                      map[string]interface{}{"type": "string"},
					"from-port":                      map[string]interface{}{"$ref": "#/definitions/valid-port"},
					"ip-protocol":                    map[string]interface{}{"type": "string"},
					"to-port":                        map[string]interface{}{"$ref": "#/definitions/valid-port"},
					"source-security-group-id":       map[string]interface{}{"type": "string"},
					"source-security-group-owner-id": map[string]interface{}{"type": "string"},
				},
				"oneOf": []interface{}{
					map[string]interface{}{"required": []interface{}{"cidr-ip"}},
					map[string]interface{}{"required": []interface{}{"cidr-ipv6"}},
					map[string]interface{}{"required": []interface{}{"source-security-group-id"}},
				},
				"required": []interface{}{"from-port", "to-port"},
			},
			"non-empty-string": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
			"parameter-group": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cluster-parameters": map[string]interface{}{"type": "object"},
					"family":             map[string]interface{}{"type": "string"},
					"parameters":         map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"family"},
			},
			"replica": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"extra-security-groups": map[string]interface{}{"type": "array"},
					"instance-class":        map[string]interface{}{"type": "string"},
					"network":               map[string]interface{}{"$ref": "#/definitions/network"},
					"parameter-group":       map[string]interface{}{"$ref": "#/definitions/parameter-group"},
					"region":                map[string]interface{}{"type": "string"},
					"tags":                  map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"instance-class", "network"},
			},
			"scaling-configuration": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"auto-pause":               map[string]interface{}{"type": "boolean"},
					"max-capacity":             map[string]interface{}{"type": "integer"},
					"min-capacity":             map[string]interface{}{"type": "integer"},
					"seconds-until-auto-pause": map[string]interface{}{"type": "integer"},
				},
			},
			"network": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"egress-rules": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"$ref": "#/definitions/egress-rule"},
					},
					"extra-security-groups": map[string]interface{}{
						"type": "array",
					},
					"ingress-rules": map[string]interface{}{
						"type":     "array",
						"items":    map[string]interface{}{"$ref": "#/definitions/ingress-rule"},
						"minItems": 1,
					},
				},
				"required": []interface{}{"ingress-rules"},
			},
			"storage": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"encrypted":  map[string]interface{}{"type": "boolean"},
					"iops":       map[string]interface{}{"type": "integer"},
					"kms-key-id": map[string]interface{}{"type": "string"},
					"size":       map[string]interface{}{"type": "integer"},
					"type":       map[string]interface{}{"type": "string"},
				},
			},
			"valid-port": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
				"maximum": 65535,
			},
		},
	}
	inputSchemaLoader := gojsonschema.NewGoLoader(schema)
	document := gojsonschema.NewGoLoader(args)
	result, err := gojsonschema.Validate(inputSchemaLoader, document)
	if err != nil {
		return err
	}
	if !result.Valid() {
		for _, e := range result.Errors() {
			err = multierror.Append(err, fmt.Errorf("%s: %s", e.Field(), e.Description()))
		}
	}
	return err
}

func decode(args map[string]interface{}) (*sqldb, error) {
	// Set initial defaults before decoding.
	sqldb := sqldb{
		MonitoringInterval: util.IntP(0),
		MultiAz:            util.BoolP(false),
		Network: &network{
			EgressRules:         []egressRule{},
			ExtraSecurityGroups: []string{},
		},
		Replicas: []replica{},
		Storage:  &storage{Encrypted: util.BoolP(true)},
		Tags:     map[string]string{},
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused: true,
		Result:      &sqldb,
	})
	if err != nil {
		return nil, err
	}
	err = decoder.Decode(args)
	if err != nil {
		return nil, err
	}

	// Set additional defaults after decoding.
	if sqldb.Dns != nil && sqldb.Dns.Ttl == nil {
		sqldb.Dns.Ttl = util.IntP(15)
	}
	if len(sqldb.Replicas) > 0 {
		for _, replica := range sqldb.Replicas {
			if replica.Network.EgressRules == nil {
				replica.Network.EgressRules = []egressRule{}
			}
			if replica.Network.ExtraSecurityGroups == nil {
				replica.Network.ExtraSecurityGroups = []string{}
			}
			if replica.Tags == nil {
				replica.Tags = map[string]string{}
			}
		}
	}

	return &sqldb, nil
}
