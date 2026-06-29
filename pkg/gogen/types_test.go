package gogen

import (
	"testing"
)

func TestPointerType(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{"string", "*string"},
		{"int64", "*int64"},
		{"bool", "*bool"},
		{"float64", "*float64"},
		{"[]string", "*[]string"},
		{"map[string]string", "*map[string]string"},
		{"map[string][]string", "*map[string][]string"},
		{"any", "any"},
	}
	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			got := PointerType(tt.goType)
			if got != tt.want {
				t.Errorf("PointerType(%q) = %q, want %q", tt.goType, got, tt.want)
			}
		})
	}
}

func TestUBTag(t *testing.T) {
	got := UBTag("BucketName")
	if got != "bucket-name" {
		t.Errorf("UBTag(BucketName) = %q, want \"bucket-name\"", got)
	}
}

func TestToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"S3Bucket", "s3_bucket"},
		{"LogGroup", "log_group"},
		{"VPCEndpoint", "vpc_endpoint"},
		{"Name", "name"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSnake(tt.input)
			if got != tt.want {
				t.Errorf("toSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
