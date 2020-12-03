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

package cluster

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/awslabs/goformation/v4/cloudformation"
	"github.com/awslabs/goformation/v4/cloudformation/autoscaling"
	"github.com/awslabs/goformation/v4/cloudformation/ec2"
	"github.com/awslabs/goformation/v4/cloudformation/elasticloadbalancingv2"
	"github.com/awslabs/goformation/v4/cloudformation/policies"
	"github.com/awslabs/goformation/v4/cloudformation/route53"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
	"github.com/mitchellh/mapstructure"
)

const (
	moduleName  = "cluster"
	application = "application"
	http        = "HTTP"
	https       = "HTTPS"
	network     = "network"
	layer4      = "layer-4"
	layer7      = "layer-7"
	pt15m       = "PT15M"
	tcp         = "TCP"
	tcpUdp      = "TCP_UDP"
	tls         = "TLS"
	udp         = "UDP"
)

type Cluster struct {
	Format        string
	LoadBalancers []interface{}
	Machines      map[string]interface{}
	StackName     string
	VpcId         string
	cluster       *cluster
	template      *cloudformation.Template
}

type cluster struct {
	Machines      machines
	LoadBalancers []loadBalancer
}

type machines struct {
	BlockDeviceMappings []blockDeviceMapping `mapstructure:"block-device-mappings,omitempty"`
	EgressRules         []egressRule         `mapstructure:"egress-rules,omitempty"`
	ExtraSecurityGroups []string             `mapstructure:"extra-security-groups,omitempty"`
	ExtraTags           map[string]string    `mapstructure:"extra-tags,omitempty"`
	IamInstanceProfile  *string              `mapstructure:"iam-instance-profile,omitempty"`
	ImageId             *string              `mapstructure:"image-id,omitempty"`
	IngressRules        []ingressRule        `mapstructure:"ingress-rules,omitempty"`
	InstanceType        *string              `mapstructure:"instance-type,omitempty"`
	KeyName             *string              `mapstructure:"key-name,omitempty"`
	MinCount            *int64               `mapstructure:"min-count,omitempty"`
	MaxCount            *int64               `mapstructure:"max-count,omitempty"`
	PlacementTenancy    *string              `mapstructure:"placement-tenancy,omitempty"`
	Provisioner         *provisioner         `mapstructure:"provisioner,omitempty"`
	Subnets             []string             `mapstructure:"subnets,omitempty"`
	TargetGroupArns     []string             `mapstructure:"target-group-arns,omitempty"`
	UpdatePolicy        *updatePolicy        `mapstructure:"update-policy,omitempty"`
	UserData            *userData            `mapstructure:"user-data,omitempty"`
}

type loadBalancer struct {
	Attributes   []attribute   `mapstructure:"attributes,omitempty"`
	Dns          *dns          `mapstructure:"dns,omitempty"`
	EgressRules  []egressRule  `mapstructure:"egress-rules,omitempty"`
	IngressRules []ingressRule `mapstructure:"ingress-rules,omitempty"`
	Listeners    []listener    `mapstructure:"listeners,omitempty"`
	Scheme       *string       `mapstructure:"scheme,omitempty"`
	Subnets      []string      `mapstructure:"subnets,omitempty"`
	Type         *string       `mapstructure:"type,omitempty"`
}

type blockDeviceMapping struct {
	DeviceName  *string `mapstructure:"device-name,omitempty"`
	Ebs         *ebs    `mapstructure:"ebs,omitempty"`
	NoDevice    *string `mapstructure:"no-device,omitempty"`
	VirtualName *string `mapstructure:"virtual-name,omitempty"`
}

type ebs struct {
	DeleteOnTermination *bool   `mapstructure:"delete-on-termination,omitempty"`
	Encrypted           *bool   `mapstructure:"encrypted,omitempty"`
	Iops                *int64  `mapstructure:"iops,omitempty"`
	KmsKeyId            *string `mapstructure:"kms-key-id,omitempty"`
	VolumeSize          *int64  `mapstructure:"volume-size,omitempty"`
	VolumeType          *string `mapstructure:"volume-type,omitempty"`
}

type provisioner struct {
	CfSignal *bool
	Raw      *rawProvisioner `mapstructure:"raw,omitempty"`
	Timeout  *string
}

type rawProvisioner struct {
	Content *string `mapstructure:"content,omitempty"`
}

type updatePolicy struct {
	AutoScalingReplacingUpdate *autoScalingReplacingUpdate `mapstructure:"auto-scaling-replacing-update,omitempty"`
	AutoScalingRollingUpdate   *autoScalingRollingUpdate   `mapstructure:"auto-scaling-rolling-update,omitempty"`
	AutoScalingScheduledAction *autoScalingScheduledAction `mapstructure:"auto-scaling-scheduled-action,omitempty"`
}

type autoScalingReplacingUpdate struct {
	WillReplace *bool `mapstructure:"will-replace,omitempty"`
}

type autoScalingRollingUpdate struct {
	MaxBatchSize                  *int64   `mapstructure:"max-batch-size,omitempty"`
	MinInstancesInService         *int64   `mapstructure:"min-instances-in-service,omitempty"`
	MinSuccessfulInstancesPercent *int64   `mapstructure:"min-successful-instances-percent,omitempty"`
	PauseTime                     *string  `mapstructure:"pause-time,omitempty"`
	SuspendProcesses              []string `mapstructure:"suspend-processes,omitempty"`
	WaitOnResourceSignals         *bool    `mapstructure:"wait-on-resource-signals,omitempty"`
}

type autoScalingScheduledAction struct {
	IgnoreUnmodifiedGroupSizeProperties *bool `mapstructure:"ignore-unmodified-group-size-properties,omitempty"`
}

type userData struct {
	Content      *string `mapstructure:"content,omitempty"`
	Base64Encode *bool   `mapstructure:"base64-encode,omitempty"`
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

type listener struct {
	ListenPort            *int64       `mapstructure:"listen-port,omitempty"`
	ListenProtocol        *string      `mapstructure:"listen-protocol,omitempty"`
	HealthCheck           *healthCheck `mapstructure:"health-check,omitempty"`
	TargetPort            *int64       `mapstructure:"target-port,omitempty"`
	TargetProtocol        *string      `mapstructure:"target-protocol,omitempty"`
	TargetGroupAttributes []attribute  `mapstructure:"target-group-attributes,omitempty"`
	CertificateArn        *string      `mapstructure:"certificate-arn,omitempty"`
	SslPolicy             *string      `mapstructure:"ssl-policy,omitempty"`
}

type healthCheck struct {
	Port     *int64  `mapstructure:"port,omitempty"`
	Protocol *string `mapstructure:"protocol,omitempty"`
	Path     *string `mapstructure:"path,omitempty"`
	Matcher  *int64  `mapstructure:"matcher,omitempty"`
}

type dns struct {
	Domain   *string `mapstructure:"domain,omitempty"`
	Hostname *string `mapstructure:"hostname,omitempty"`
}

type attribute struct {
	Key   *string `mapstructure:"key,omitempty"`
	Value *string `mapstructure:"value,omitempty"`
}

func (c *Cluster) Initialize() error {
	if c.StackName == "" {
		return errors.New("stack-name must be defined")
	}
	if c.Format == "" {
		c.Format = "yaml"
	} else {
		err := util.IsOneOfString(c.Format, "format", []string{"json", "yaml"}, true)
		if err != nil {
			return err
		}
	}
	if c.VpcId == "" {
		return errors.New("vpc-id must be defined")
	}
	if c.Machines == nil {
		return errors.New("machines must be defined")
	}
	var machines machines
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused: true,
		Result:      &machines,
	})
	if err != nil {
		return err
	}
	err = decoder.Decode(c.Machines)
	if err != nil {
		return err
	}
	err = validateMachines(&machines)
	if err != nil {
		return err
	}
	var loadBalancers []loadBalancer
	decoder, err = mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused: true,
		Result:      &loadBalancers,
	})
	if err != nil {
		return err
	}
	err = decoder.Decode(c.LoadBalancers)
	if err != nil {
		return err
	}
	if len(loadBalancers) > 0 {
		if machines.TargetGroupArns != nil {
			return errors.New("either target-group-arns or load-balancers can be specified")
		}
		for i := range loadBalancers {
			err := validateLoadBalancer(&loadBalancers[i])
			if err != nil {
				return err
			}
		}
	}
	c.cluster = &cluster{Machines: machines, LoadBalancers: loadBalancers}
	c.template = cloudformation.NewTemplate()
	return nil
}

func (c *Cluster) Name() string {
	return moduleName
}

func (c *Cluster) Apply() *types.Result {
	c.defineTemplateLoadBalancers()
	c.defineTemplateMachines()
	c.defineTemplateOutputs()

	template, err := c.generateTemplate()
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

func (c *Cluster) Destroy() *types.Result {
	return nil
}

func (c *Cluster) defineTemplateLoadBalancers() {
	for i, lb := range c.cluster.LoadBalancers {
		securityGroupKey := fmt.Sprintf("LoadBalancer%dSecurityGroup", i)
		if *lb.Type == layer7 {
			c.template.Resources[securityGroupKey] = &ec2.SecurityGroup{
				GroupDescription: fmt.Sprintf("Security group for %s-lb-%d", c.StackName, i),
				VpcId:            c.VpcId,
			}
			for j, egress := range lb.EgressRules {
				egressRsc := &ec2.SecurityGroupEgress{
					FromPort:   int(*egress.FromPort),
					GroupId:    cloudformation.GetAtt(securityGroupKey, "GroupId"),
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
				egressKey := fmt.Sprintf("LoadBalancer%dEgress%d", i, j)
				c.template.Resources[egressKey] = egressRsc
			}
			for j, ingress := range lb.IngressRules {
				ingressRsc := &ec2.SecurityGroupIngress{
					FromPort:   int(*ingress.FromPort),
					GroupId:    cloudformation.GetAtt(securityGroupKey, "GroupId"),
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
				if ingress.SourceSecurityGroupOwnerId != nil {
					ingressRsc.SourceSecurityGroupOwnerId = *ingress.SourceSecurityGroupOwnerId
				}
				ingressKey := fmt.Sprintf("LoadBalancer%dIngress%d", i, j)
				c.template.Resources[ingressKey] = ingressRsc
			}
		}

		lbKey := fmt.Sprintf("LoadBalancer%d", i)
		for j, listener := range lb.Listeners {
			targetGroupKey := fmt.Sprintf("LoadBalancer%dTargetGroup%d", i, j)
			targetGroupRsc := &elasticloadbalancingv2.TargetGroup{
				Name:                fmt.Sprintf("%s-lb-%d-tg-%d", c.StackName, i, j),
				HealthCheckEnabled:  true,
				HealthCheckPort:     fmt.Sprint(*listener.HealthCheck.Port),
				HealthCheckProtocol: *listener.HealthCheck.Protocol,
				Port:                int(*listener.TargetPort),
				Protocol:            *listener.TargetProtocol,
				VpcId:               c.VpcId,
			}
			if listener.HealthCheck.Path != nil {
				targetGroupRsc.HealthCheckPath = *listener.HealthCheck.Path
			}
			if listener.HealthCheck.Matcher != nil {
				matcher := &elasticloadbalancingv2.TargetGroup_Matcher{
					HttpCode: fmt.Sprint(*listener.HealthCheck.Matcher),
				}
				targetGroupRsc.Matcher = matcher
			}
			if listener.TargetGroupAttributes != nil {
				attributes := make([]elasticloadbalancingv2.TargetGroup_TargetGroupAttribute,
					len(listener.TargetGroupAttributes))
				for k, attribute := range targetGroupRsc.TargetGroupAttributes {
					attributes[k] = elasticloadbalancingv2.TargetGroup_TargetGroupAttribute{
						Key:   attribute.Key,
						Value: attribute.Value,
					}
				}
				targetGroupRsc.TargetGroupAttributes = attributes
			}
			c.template.Resources[targetGroupKey] = targetGroupRsc

			listenerKey := fmt.Sprintf("LoadBalancer%dListener%d", i, j)
			listenerRsc := &elasticloadbalancingv2.Listener{
				DefaultActions: []elasticloadbalancingv2.Listener_Action{
					{
						TargetGroupArn: cloudformation.Ref(targetGroupKey),
						Type:           "forward",
					},
				},
				LoadBalancerArn: cloudformation.Ref(lbKey),
				Port:            int(*listener.ListenPort),
				Protocol:        *listener.ListenProtocol,
			}
			if listener.CertificateArn != nil {
				listenerRsc.Certificates = []elasticloadbalancingv2.Listener_Certificate{
					{
						CertificateArn: *listener.CertificateArn,
					},
				}
			}
			if listener.SslPolicy != nil {
				listenerRsc.SslPolicy = *listener.SslPolicy
			}
			c.template.Resources[listenerKey] = listenerRsc
		}
		attributes := make([]elasticloadbalancingv2.LoadBalancer_LoadBalancerAttribute, len(lb.Attributes))
		for j, attribute := range lb.Attributes {
			attributes[j] = elasticloadbalancingv2.LoadBalancer_LoadBalancerAttribute{
				Key:   *attribute.Key,
				Value: *attribute.Value,
			}
		}
		lbRsc := &elasticloadbalancingv2.LoadBalancer{
			Name:                   fmt.Sprintf("%s-lb-%d", c.StackName, i),
			LoadBalancerAttributes: attributes,
			Scheme:                 *lb.Scheme,
			Subnets:                lb.Subnets,
		}
		if *lb.Type == layer4 {
			lbRsc.Type = network
		}
		if *lb.Type == layer7 {
			lbRsc.SecurityGroups = []string{cloudformation.GetAtt(securityGroupKey, "GroupId")}
			lbRsc.Type = application
		}
		c.template.Resources[lbKey] = lbRsc
		if lb.Dns != nil {
			dnsKey := fmt.Sprintf("DnsRecord%d", i)
			dnsRsc := &route53.RecordSet{
				AliasTarget: &route53.RecordSet_AliasTarget{
					DNSName:      *lb.Dns.Hostname,
					HostedZoneId: cloudformation.GetAtt(lbKey, "CanonicalHostedZoneID"),
				},
				HostedZoneName: fmt.Sprintf("%s.", *lb.Dns.Domain),
				Name:           fmt.Sprintf("%s.%s", *lb.Dns.Hostname, *lb.Dns.Domain),
				Type:           "A",
			}
			c.template.Resources[dnsKey] = dnsRsc
		}
	}
}

func (c *Cluster) defineTemplateMachines() {
	asgSecurityGroupKey := "AutoscalingGroupSecurityGroup"
	asgSecurityGroupRsc := &ec2.SecurityGroup{
		GroupDescription: fmt.Sprintf("Security group for %s machines", c.StackName),
	}
	c.template.Resources[asgSecurityGroupKey] = asgSecurityGroupRsc
	for i, egress := range c.cluster.Machines.EgressRules {
		egressRsc := &ec2.SecurityGroupEgress{
			FromPort:   int(*egress.FromPort),
			GroupId:    cloudformation.GetAtt(asgSecurityGroupKey, "GroupId"),
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
		egressKey := fmt.Sprintf("AutoscalingGroupEgress%d", i)
		c.template.Resources[egressKey] = egressRsc
	}
	for i, ingress := range c.cluster.Machines.IngressRules {
		ingressRsc := &ec2.SecurityGroupIngress{
			FromPort:   int(*ingress.FromPort),
			GroupId:    cloudformation.GetAtt(asgSecurityGroupKey, "GroupId"),
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
		if ingress.SourceSecurityGroupOwnerId != nil {
			ingressRsc.SourceSecurityGroupOwnerId = *ingress.SourceSecurityGroupOwnerId
		}
		ingressKey := fmt.Sprintf("AutoscalingGroupIngress%d", i)
		c.template.Resources[ingressKey] = ingressRsc
	}

	launchTemplateKey := "LaunchTemplate"
	launchTemplateData := &ec2.LaunchTemplate_LaunchTemplateData{
		ImageId:      *c.cluster.Machines.ImageId,
		InstanceType: *c.cluster.Machines.InstanceType,
		KeyName:      *c.cluster.Machines.KeyName,
	}
	securityGroups := make([]string, len(c.cluster.Machines.ExtraSecurityGroups)+1)
	securityGroups[0] = cloudformation.GetAtt(asgSecurityGroupKey, "GroupId")
	for i, securityGroup := range c.cluster.Machines.ExtraSecurityGroups {
		securityGroups[i+1] = securityGroup
	}
	launchTemplateData.SecurityGroupIds = securityGroups
	if c.cluster.Machines.IamInstanceProfile != nil {
		launchTemplateData.IamInstanceProfile = &ec2.LaunchTemplate_IamInstanceProfile{
			Name: *c.cluster.Machines.IamInstanceProfile,
		}
	}
	if c.cluster.Machines.UserData != nil {
		if *c.cluster.Machines.UserData.Base64Encode {
			bites := []byte(*c.cluster.Machines.UserData.Content)
			launchTemplateData.UserData = base64.StdEncoding.EncodeToString(bites)
		} else {
			launchTemplateData.UserData = *c.cluster.Machines.UserData.Content
		}
	}
	launchTemplateRsc := &ec2.LaunchTemplate{
		LaunchTemplateData: launchTemplateData,
	}
	c.template.Resources[launchTemplateKey] = launchTemplateRsc

	asgKey := "AutoscalingGroup"
	asgRsc := &autoscaling.AutoScalingGroup{
		AutoScalingGroupName: cloudformation.Ref("AWS::StackName"),
		LaunchTemplate: &autoscaling.AutoScalingGroup_LaunchTemplateSpecification{
			LaunchTemplateId: cloudformation.Ref(launchTemplateKey),
			Version:          cloudformation.GetAtt(launchTemplateKey, "LatestVersionNumber"),
		},
		MaxSize:           fmt.Sprint(*c.cluster.Machines.MaxCount),
		MinSize:           fmt.Sprint(*c.cluster.Machines.MinCount),
		VPCZoneIdentifier: c.cluster.Machines.Subnets,
	}
	if len(c.cluster.LoadBalancers) != 0 {
		targetGroupARNs := []string{}
		for i, lb := range c.cluster.LoadBalancers {
			for j := range lb.Listeners {
				targetGroupKey := fmt.Sprintf("LoadBalancer%dTargetGroup%d", i, j)
				targetGroupARNs = append(targetGroupARNs, cloudformation.Ref(targetGroupKey))
			}
		}
		asgRsc.TargetGroupARNs = targetGroupARNs
	}
	if c.cluster.Machines.TargetGroupArns != nil {
		asgRsc.TargetGroupARNs = c.cluster.Machines.TargetGroupArns
	}
	tags := []autoscaling.AutoScalingGroup_TagProperty{
		{Key: "Name", Value: c.StackName, PropagateAtLaunch: true},
	}
	for k, v := range c.cluster.Machines.ExtraTags {
		tags = append(tags, autoscaling.AutoScalingGroup_TagProperty{Key: k, Value: v, PropagateAtLaunch: true})
	}
	asgRsc.Tags = tags
	if c.cluster.Machines.Provisioner.CfSignal != nil && *c.cluster.Machines.Provisioner.CfSignal {
		asgRsc.AWSCloudFormationCreationPolicy = &policies.CreationPolicy{
			ResourceSignal: &policies.ResourceSignal{
				Count:   float64(*c.cluster.Machines.MinCount),
				Timeout: *c.cluster.Machines.Provisioner.Timeout,
			},
		}
	}
	if c.cluster.Machines.UpdatePolicy != nil {
		asgRsc.AWSCloudFormationUpdatePolicy = &policies.UpdatePolicy{}
		if c.cluster.Machines.UpdatePolicy.AutoScalingReplacingUpdate != nil {
			policy := &policies.AutoScalingReplacingUpdate{
				WillReplace: *c.cluster.Machines.UpdatePolicy.AutoScalingReplacingUpdate.WillReplace,
			}
			asgRsc.AWSCloudFormationUpdatePolicy.AutoScalingReplacingUpdate = policy
		}
		if c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate != nil {
			policy := &policies.AutoScalingRollingUpdate{
				MaxBatchSize:                  float64(*c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate.MaxBatchSize),
				MinInstancesInService:         float64(*c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate.MinInstancesInService),
				MinSuccessfulInstancesPercent: float64(*c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate.MinSuccessfulInstancesPercent),
				PauseTime:                     fmt.Sprint(*c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate.PauseTime),
				SuspendProcesses:              c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate.SuspendProcesses,
				WaitOnResourceSignals:         *c.cluster.Machines.UpdatePolicy.AutoScalingRollingUpdate.WaitOnResourceSignals,
			}
			asgRsc.AWSCloudFormationUpdatePolicy.AutoScalingRollingUpdate = policy
		}
		if c.cluster.Machines.UpdatePolicy.AutoScalingScheduledAction != nil {
			policy := &policies.AutoScalingScheduledAction{
				IgnoreUnmodifiedGroupSizeProperties: *c.cluster.Machines.UpdatePolicy.AutoScalingScheduledAction.IgnoreUnmodifiedGroupSizeProperties,
			}
			asgRsc.AWSCloudFormationUpdatePolicy.AutoScalingScheduledAction = policy
		}
	}
	c.template.Resources[asgKey] = asgRsc
}

func (c *Cluster) defineTemplateOutputs() {
	for i, lb := range c.cluster.LoadBalancers {
		if *lb.Type == layer7 {
			securityGroupKey := fmt.Sprintf("LoadBalancer%dSecurityGroup", i)
			c.template.Outputs[securityGroupKey] = cloudformation.Output{
				// Exports are required as they aren't pointers and can't be nil.
				// See https://github.com/awslabs/goformation/pull/299.
				Export: cloudformation.Export{Name: securityGroupKey},
				Value:  cloudformation.GetAtt(securityGroupKey, "GroupId"),
			}
		}
		amzDnsKey := fmt.Sprintf("LoadBalancer%dAmazonDns", i)
		lbKey := fmt.Sprintf("LoadBalancer%d", i)
		c.template.Outputs[amzDnsKey] = cloudformation.Output{
			Export: cloudformation.Export{Name: amzDnsKey},
			Value:  cloudformation.GetAtt(lbKey, "DNSName"),
		}
		for j := range lb.Listeners {
			targetGroupKey := fmt.Sprintf("LoadBalancer%dTargetGroup%d", i, j)
			c.template.Outputs[targetGroupKey] = cloudformation.Output{
				Export: cloudformation.Export{Name: targetGroupKey},
				Value:  cloudformation.Ref(targetGroupKey),
			}
		}
	}
	asgKey := "AutoscalingGroup"
	c.template.Outputs[asgKey] = cloudformation.Output{
		Export: cloudformation.Export{Name: asgKey},
		Value:  cloudformation.Ref(asgKey),
	}
	asgSecurityGroupKey := "AutoscalingGroupSecurityGroup"
	c.template.Outputs[asgSecurityGroupKey] = cloudformation.Output{
		Export: cloudformation.Export{Name: asgSecurityGroupKey},
		Value:  cloudformation.GetAtt(asgSecurityGroupKey, "GroupId"),
	}
}

func (c *Cluster) generateTemplate() (string, error) {
	if strings.ToLower(c.Format) == "json" {
		template, err := c.template.JSON()
		if err != nil {
			return "", err
		}
		return string(template), nil
	}
	if strings.ToLower(c.Format) == "yaml" {
		template, err := c.template.YAML()
		if err != nil {
			return "", err
		}
		return string(template), nil
	}
	// Should not reach here since c.Format has been validated
	// to be either json or yaml.
	return "", errors.New("invalid template format")
}

func validateMachines(machines *machines) error {
	for i := range machines.EgressRules {
		err := validateEgressRule(&machines.EgressRules[i])
		if err != nil {
			return err
		}
	}
	for i := range machines.IngressRules {
		err := validateIngressRule(&machines.IngressRules[i])
		if err != nil {
			return err
		}
	}
	if machines.ImageId == nil {
		return errors.New("image-id must be defined for machines")
	}
	if machines.InstanceType == nil {
		return errors.New("instance-type must be defined for machines")
	}
	if machines.KeyName == nil {
		return errors.New("key-name must be defined for machines")
	}
	if machines.MaxCount == nil {
		return errors.New("max-count must be defined for machines")
	}
	if machines.MinCount == nil {
		return errors.New("min-count must be defined for machines")
	}
	if machines.PlacementTenancy == nil {
		machines.PlacementTenancy = util.StringP("default")
	}
	err := validateProvisioner(machines.Provisioner)
	if err != nil {
		return err
	}
	if len(machines.Subnets) == 0 {
		return errors.New("subnets must be defined for machines")
	}
	err = validateUpdatePolicy(machines.UpdatePolicy)
	if err != nil {
		return err
	}
	if machines.UserData != nil {
		if machines.UserData.Base64Encode == nil {
			machines.UserData.Base64Encode = util.BoolP(false)
		}
	}
	return nil
}

func validateProvisioner(provisioner *provisioner) error {
	if provisioner == nil {
		return errors.New("provisioner must be defined for machines")
	}
	if provisioner.CfSignal == nil {
		provisioner.CfSignal = util.BoolP(false)
	}
	if provisioner.Timeout == nil {
		provisioner.Timeout = util.StringP("PT15M")
	}
	return nil
}

func validateUpdatePolicy(updatePolicy *updatePolicy) error {
	if updatePolicy == nil {
		return nil
	}
	nils := 0
	if updatePolicy.AutoScalingReplacingUpdate == nil {
		nils++
	}
	if updatePolicy.AutoScalingRollingUpdate == nil {
		nils++
	}
	if updatePolicy.AutoScalingScheduledAction == nil {
		nils++
	}
	if nils != 2 {
		validChoices := []string{
			"auto-scaling-replacing-update",
			"auto-scaling-rolling-update",
			"auto-scaling-scheduled-action",
		}
		return fmt.Errorf("provisioner update policy must contain one of %s", strings.Join(validChoices, ", "))
	}
	if updatePolicy.AutoScalingReplacingUpdate != nil {
		if updatePolicy.AutoScalingReplacingUpdate.WillReplace == nil {
			return errors.New("will-replace must be defined for auto scaling replacing update")
		}
	}
	if updatePolicy.AutoScalingRollingUpdate != nil {
		if updatePolicy.AutoScalingRollingUpdate.MaxBatchSize == nil {
			return errors.New("max-batch-size must be defined for auto scaling rolling update")
		}
		if updatePolicy.AutoScalingRollingUpdate.MinInstancesInService == nil {
			return errors.New("min-instances-in-service be defined for auto scaling rolling update")
		}
		if updatePolicy.AutoScalingRollingUpdate.MinSuccessfulInstancesPercent == nil {
			updatePolicy.AutoScalingRollingUpdate.MinSuccessfulInstancesPercent = util.IntP(100)
		}
		if updatePolicy.AutoScalingRollingUpdate.PauseTime == nil {
			updatePolicy.AutoScalingRollingUpdate.PauseTime = util.StringP(pt15m)
		}
		if updatePolicy.AutoScalingRollingUpdate.WaitOnResourceSignals == nil {
			updatePolicy.AutoScalingRollingUpdate.WaitOnResourceSignals = util.BoolP(false)
		}
	}
	if updatePolicy.AutoScalingScheduledAction != nil {
		if updatePolicy.AutoScalingScheduledAction.IgnoreUnmodifiedGroupSizeProperties == nil {
			return errors.New("ignore-unmodified-group-size-properties must be defined for auto scaling scheduled action")
		}
	}
	return nil
}

func validateLoadBalancer(lb *loadBalancer) error {
	if *lb.Type == layer4 {
		if lb.EgressRules != nil {
			return errors.New("layer-4 load balancer must not have egress-rules")
		}
		if lb.IngressRules != nil {
			return errors.New("layer-4 load balancer must not have ingress-rules")
		}
	} else {
		for i := range lb.EgressRules {
			err := validateEgressRule(&lb.EgressRules[i])
			if err != nil {
				return err
			}
		}
		for i := range lb.IngressRules {
			err := validateIngressRule(&lb.IngressRules[i])
			if err != nil {
				return err
			}
		}
	}
	if lb.Dns != nil {
		if lb.Dns.Domain == nil {
			return errors.New("domain must be defined for load balancer dns")
		}
		if lb.Dns.Hostname == nil {
			return errors.New("hostname must be defined for load balancer dns")
		}
	}
	if len(lb.Listeners) == 0 {
		return errors.New("listeners must be defined for load balancer")
	}
	for _, listener := range lb.Listeners {
		err := validateListener(*lb.Type, &listener)
		if err != nil {
			return err
		}
	}
	if lb.Scheme == nil {
		lb.Scheme = util.StringP("internal")
	}
	if lb.Subnets == nil {
		return errors.New("subnets must be defined for load balancer")
	}
	if lb.Type == nil {
		return errors.New("type must be defined for load balancer")
	}
	err := util.IsOneOfString(*lb.Type, "load balancer type", []string{layer4, layer7}, true)
	if err != nil {
		return err
	}
	return nil
}

func validateEgressRule(rule *egressRule) error {
	nils := 0
	if rule.CidrIp == nil {
		nils++
	}
	if rule.CidrIpv6 == nil {
		nils++
	}
	if rule.DestinationPrefixListId == nil {
		nils++
	}
	if rule.DestinationSecurityGroupId == nil {
		nils++
	}
	if nils != 3 {
		validChoices := []string{
			"cidr-ip",
			"cidr-ipv6",
			"destination-prefix-list-id",
			"destination-security-group-id",
		}
		return fmt.Errorf("egress rule must contain one of %s", strings.Join(validChoices, ", "))
	}
	err := validatePort("from-port", "egress rule", rule.FromPort)
	if err != nil {
		return err
	}
	if rule.IpProtocol == nil {
		rule.IpProtocol = util.StringP(tcp)
	}
	err = validatePort("to-port", "egress rule", rule.ToPort)
	return err
}

func validateIngressRule(rule *ingressRule) error {
	nils := 0
	if rule.CidrIp == nil {
		nils++
	}
	if rule.CidrIpv6 == nil {
		nils++
	}
	if rule.SourceSecurityGroupId == nil {
		nils++
	}
	if nils != 2 {
		validChoices := []string{
			"cidr-ip",
			"cidr-ipv6",
			"source-security-group-id",
		}
		return fmt.Errorf("ingress rule must contain one of %s", strings.Join(validChoices, ", "))
	}
	err := validatePort("from-port", "ingress rule", rule.FromPort)
	if err != nil {
		return err
	}
	if rule.IpProtocol == nil {
		rule.IpProtocol = util.StringP(tcp)
	}
	err = validatePort("to-port", "ingress rule", rule.ToPort)
	return err
}

func validateListener(lbType string, listener *listener) error {
	err := validatePort("listen-port", "load balancer listener", listener.ListenPort)
	if err != nil {
		return err
	}
	if listener.ListenProtocol == nil {
		return errors.New("listen-protocol must be defined for load balancer listener")
	}
	if listener.HealthCheck == nil {
		return errors.New("health-check must be defined for load balancer listener")
	}
	err = validatePort("port", "load balancer listener health check", listener.HealthCheck.Port)
	if err != nil {
		return err
	}
	if listener.HealthCheck.Protocol == nil {
		listener.HealthCheck.Protocol = listener.ListenProtocol
	}
	if listener.HealthCheck.Path == nil {
		if *listener.ListenProtocol == http || *listener.ListenProtocol == https {
			listener.HealthCheck.Path = util.StringP("/")
		}
	}
	if listener.HealthCheck.Matcher == nil {
		if *listener.ListenProtocol == http || *listener.ListenProtocol == https {
			listener.HealthCheck.Matcher = util.IntP(200)
		}
	}
	if listener.SslPolicy == nil {
		if *listener.ListenProtocol == http || *listener.ListenProtocol == https {
			listener.SslPolicy = util.StringP("ELBSecurityPolicy-2016-08")
		}
	}
	err = validatePort("target-port", "load balancer listener", listener.TargetPort)
	if err != nil {
		return err
	}
	if listener.TargetProtocol == nil {
		return errors.New("target-protocol must be defined for load balancer listener")
	}
	if lbType == layer7 {
		validProtocols := []string{http, https}
		err := util.IsOneOfString(*listener.TargetProtocol, "listener target protocol", validProtocols, false)
		if err != nil {
			return err
		}
	} else if lbType == layer4 {
		validProtocols := []string{tcp, tls, udp, tcpUdp}
		err := util.IsOneOfString(*listener.TargetProtocol, "listener target protocol", validProtocols, false)
		if err != nil {
			return err
		}
	}

	return nil
}

func validatePort(attribute, resource string, port *int64) error {
	if port == nil {
		return fmt.Errorf("%s must be defined for %s", attribute, resource)
	}
	if *port < 0 || *port > 65535 {
		return fmt.Errorf("%s must be between 0 and 65535", attribute)
	}
	return nil
}
