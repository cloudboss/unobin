package tf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type staticFetcher struct {
	data []byte
}

func (s *staticFetcher) FetchSchema(_ context.Context, _, _, _ string) ([]byte, error) {
	return s.data, nil
}

func TestConvertS3Bucket(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "aws_provider.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	fetcher := &staticFetcher{data: data}
	adapter := NewAdapter(fetcher, "hashicorp/aws", "")
	resources, err := adapter.FetchResources(context.Background(), []string{"s3"})
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	rs := resources[0]
	if rs.GoName != "S3Bucket" {
		t.Errorf("GoName = %q, want S3Bucket", rs.GoName)
	}
	if rs.CloudType != "aws_s3_bucket" {
		t.Errorf("CloudType = %q, want aws_s3_bucket", rs.CloudType)
	}

	// Input fields: bucket (required), force_destroy (optional), tags (optional)
	if len(rs.InputFields) != 3 {
		t.Errorf("expected 3 input fields, got %d", len(rs.InputFields))
	}

	findInput := func(name string) *struct{ GoType string; Required bool } {
		for _, f := range rs.InputFields {
			if f.Name == name {
				return &struct{ GoType string; Required bool }{f.GoType, f.Required}
			}
		}
		return nil
	}

	bucket := findInput("Bucket")
	if bucket == nil {
		t.Error("Bucket field not found in inputs")
	} else if !bucket.Required {
		t.Error("Bucket should be required")
	} else if bucket.GoType != "string" {
		t.Errorf("Bucket GoType = %q, want string", bucket.GoType)
	}

	force := findInput("ForceDestroy")
	if force == nil {
		t.Error("ForceDestroy field not found in inputs")
	} else if force.Required {
		t.Error("ForceDestroy should be optional")
	}

	tags := findInput("Tags")
	if tags == nil {
		t.Error("Tags field not found in inputs")
	} else if tags.GoType != "map[string]string" {
		t.Errorf("Tags GoType = %q, want map[string]string", tags.GoType)
	}

	// Output fields: arn, bucket_domain_name
	if len(rs.OutputFields) != 2 {
		t.Errorf("expected 2 output fields, got %d", len(rs.OutputFields))
	}

	findOutput := func(name string) *struct{ GoType string } {
		for _, f := range rs.OutputFields {
			if f.Name == name {
				return &struct{ GoType string }{f.GoType}
			}
		}
		return nil
	}

	arn := findOutput("ARN")
	if arn == nil {
		t.Error("ARN field not found in outputs")
	} else if arn.GoType != "string" {
		t.Errorf("ARN GoType = %q, want string", arn.GoType)
	}

}

func TestTfNameToGo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"aws_s3_bucket", "S3Bucket"},
		{"aws_ec2_instance", "Ec2Instance"},
		{"aws_lambda_function", "LambdaFunction"},
		{"aws_iam_role", "IamRole"},
		{"google_compute_instance", "ComputeInstance"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tfNameToGo(tt.input)
			if got != tt.want {
				t.Errorf("tfNameToGo(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTfTypeToGo(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`"string"`, "string"},
		{`"number"`, "float64"},
		{`"bool"`, "bool"},
		{`["list","string"]`, "[]string"},
		{`["set","string"]`, "[]string"},
		{`["map","string"]`, "map[string]string"},
		{`["list",["object",{}]]`, "[]map[string]any"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := tfTypeToGo([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("tfTypeToGo(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestServicePrefixes(t *testing.T) {
	got := resourcePrefixes("aws", []string{"s3", "ec2"})
	if len(got) != 2 || got[0] != "aws_s3" || got[1] != "aws_ec2" {
		t.Errorf("resourcePrefixes = %v, want [aws_s3, aws_ec2]", got)
	}
}

func TestMatchesPrefix(t *testing.T) {
	prefixes := []string{"aws_s3", "aws_ec2"}
	if !matchesPrefix("aws_s3_bucket", prefixes) {
		t.Error("aws_s3_bucket should match aws_s3")
	}
	if matchesPrefix("aws_lambda_function", prefixes) {
		t.Error("aws_lambda_function should NOT match aws_s3 or aws_ec2")
	}
	if !matchesPrefix("anything", nil) {
		t.Error("nil prefixes should match everything")
	}
}

func TestTfAttrNameToGo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bucket", "Bucket"},
		{"bucket_domain_name", "BucketDomainName"},
		{"force_destroy", "ForceDestroy"},
		{"vpc_id", "VpcId"},
		{"kms_key_id", "KMSKeyId"},
		{"http_endpoint", "HTTPEndpoint"},
		{"arn", "ARN"},
		// Edge cases that produce invalid Go identifiers.
		{"", ""},
		{"_", ""},
		{"___", ""},
		{"123", ""},
		{"1foo", ""},
		{"some-field", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tfAttrNameToGo(tt.input)
			if got != tt.want {
				t.Errorf("tfAttrNameToGo(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertResourceSkipsInvalidGoNames(t *testing.T) {
	rs := tfResourceSchema{
		Version: 0,
		Block: tfBlock{
			Attributes: map[string]tfAttribute{
				"valid_name": {
					Type:     json.RawMessage(`"string"`),
					Required: true,
				},
				"___": {
					Type:     json.RawMessage(`"string"`),
					Optional: true,
				},
				"123": {
					Type:     json.RawMessage(`"string"`),
					Optional: true,
				},
			},
		},
	}

	got, err := convertResource("test_thing", rs)
	if err != nil {
		t.Fatalf("convertResource: %v", err)
	}
	if len(got.InputFields) != 1 {
		t.Errorf("expected 1 input field (skipped 2 invalid), got %d", len(got.InputFields))
	}
	if len(got.InputFields) > 0 && got.InputFields[0].Name != "ValidName" {
		t.Errorf("expected ValidName, got %q", got.InputFields[0].Name)
	}
}

func TestConvertDataSource(t *testing.T) {
	ds := tfResourceSchema{
		Version: 0,
		Block: tfBlock{
			Attributes: map[string]tfAttribute{
				"filter": {
					Type:     json.RawMessage(`"string"`),
					Required: true,
				},
				"most_recent": {
					Type:     json.RawMessage(`"bool"`),
					Optional: true,
				},
				"arn": {
					Type:     json.RawMessage(`"string"`),
					Computed: true,
				},
			},
		},
	}

	got, err := convertDataSource("aws_ami", ds)
	if err != nil {
		t.Fatalf("convertDataSource: %v", err)
	}
	if got.GoName != "Ami" {
		t.Errorf("GoName = %q, want Ami", got.GoName)
	}
	if len(got.InputFields) != 2 {
		t.Errorf("expected 2 input fields, got %d", len(got.InputFields))
	}
	if len(got.OutputFields) != 1 {
		t.Errorf("expected 1 output field, got %d", len(got.OutputFields))
	}
}
