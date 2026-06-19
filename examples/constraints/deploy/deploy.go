// Package deploy is a small demonstration library that exists so
// examples and tests can exercise Go-declared constraints. Its single
// resource (service) renders a service spec to a local file, and its
// input type declares cross-field rules from a Constraints method.
package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/constraint"
	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// Library returns the registration record for the `deploy` library.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "deploy",
		Description: "Demonstrates Go-declared constraints by rendering a service spec to a file.",
		Resources: map[string]runtime.ResourceRegistration{
			"service": runtime.MakeResource[Service, *ServiceOutput, any](),
		},
	}
}

// Service renders a service spec to a file at Path, creating parent
// directories as needed. Changing the path replaces the resource.
type Service struct {
	Name     string
	Tier     string
	Image    *string
	Build    *string
	Replicas int64
	Ports    []Port
	Path     string
}

// Port is one listening port of the service.
type Port struct {
	Number int64
	TLS    *bool
	Cert   *string
}

// Constraints declares the rules the service's inputs must satisfy.
// The fields are real struct fields, so the Go compiler checks that
// they exist; unobin reads the rules from source at compile time and
// checks every `deploy.service` body against them, exactly as it
// checks a UB constraints block.
func (s Service) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(s.Image, s.Build),
		constraint.Must(constraint.OneOf(s.Tier, "dev", "prod")).
			Message("tier must be dev or prod"),
		constraint.When(constraint.Equals(s.Tier, "prod")).
			Require(constraint.AtLeast(s.Replicas, 2)).
			Message("prod runs at least two replicas"),
		constraint.ForEach(s.Ports, func(p Port) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.When(constraint.IsTrue(p.TLS)).
					Require(constraint.Present(p.Cert)).
					Message("a tls port needs a cert"),
			}
		}),
	}
}

// ServiceOutput holds what rendering the spec computes; path is an
// input and is readable as one, so it is not copied here.
type ServiceOutput struct {
	SHA256 string
	Size   int64
}

func (s *Service) SchemaVersion() int      { return 1 }
func (s *Service) ReplaceFields() []string { return []string{"path"} }

func (s *Service) Create(_ context.Context, _ any) (*ServiceOutput, error) {
	return s.write()
}

func (s *Service) Read(_ context.Context, _ any, _ *ServiceOutput) (*ServiceOutput, error) {
	info, err := os.Stat(s.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, runtime.ErrNotFound
		}
		return nil, err
	}
	body, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return &ServiceOutput{
		SHA256: hex.EncodeToString(sum[:]),
		Size:   info.Size(),
	}, nil
}

func (s *Service) Update(
	_ context.Context, _ any, _ runtime.Prior[Service, *ServiceOutput],
) (*ServiceOutput, error) {
	return s.write()
}

func (s *Service) Delete(_ context.Context, _ any, _ *ServiceOutput) error {
	err := os.Remove(s.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Service) write() (*ServiceOutput, error) {
	if s.Path == "" {
		return nil, errors.New("deploy.service: path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return nil, err
	}
	body := []byte(s.render())
	if err := ufs.WriteFileAtomic(s.Path, body, 0o644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return &ServiceOutput{
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}, nil
}

func (s *Service) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "service %s\n", s.Name)
	fmt.Fprintf(&b, "tier: %s\n", s.Tier)
	fmt.Fprintf(&b, "replicas: %d\n", s.Replicas)
	if s.Image != nil {
		fmt.Fprintf(&b, "image: %s\n", *s.Image)
	}
	if s.Build != nil {
		fmt.Fprintf(&b, "build: %s\n", *s.Build)
	}
	for _, p := range s.Ports {
		fmt.Fprintf(&b, "port %d", p.Number)
		if p.TLS != nil && *p.TLS {
			fmt.Fprintf(&b, " tls cert=%s", *p.Cert)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
