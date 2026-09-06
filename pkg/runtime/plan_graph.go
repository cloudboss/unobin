package runtime

import "github.com/cloudboss/unobin/pkg/stateref"

// StepNode describes a plan step and its apply dependencies.
type StepNode struct {
	Address     string   `json:"address"`
	Kind        NodeKind `json:"node-kind"`
	Composite   bool     `json:"composite,omitempty"`
	Decision    Decision `json:"decision"`
	DependsOn   []string `json:"depends-on,omitempty"`
	Category    string   `json:"category,omitempty"`
	ImportAlias string   `json:"import-alias,omitempty"`
	LibraryPath string   `json:"library-path,omitempty"`
	ExportKind  string   `json:"kind,omitempty"`
	Name        string   `json:"name,omitempty"`
	Parent      string   `json:"parent,omitempty"`
}

func segmentName(segment stateref.StateAddressSegment) string {
	if segment.Key == nil {
		return segment.Name
	}
	rendered := segment.String()
	return rendered[len(string(segment.Category))+1:]
}

func stateRefParent(ref stateref.StateRef) string {
	if len(ref.Segments) <= 1 {
		return ""
	}
	return stateref.StateRef{Segments: ref.Segments[:len(ref.Segments)-1]}.String()
}
