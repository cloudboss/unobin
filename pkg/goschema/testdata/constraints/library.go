package constraints

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "tls",
		Resources: map[string]runtime.ResourceRegistration{
			"cert": runtime.MakeResource[Cert, *CertOutput](),
		},
	}
}

type Cert struct {
	SelfSigned  *bool   `ub:"self-signed,omitempty"`
	AcmArn      *string `ub:"acm-arn,omitempty"`
	PemBundle   *string `ub:"pem-bundle,omitempty"`
	PrivateKey  *string `ub:"private-key,omitempty"`
	RenewBefore *int    // no tag: kebab name derived as renew-before
	Region      string  `ub:"region"`
}

// Constraints exercises every set-constraint kind, with field selectors
// that map through both an explicit ub tag and a derived kebab name.
func (c Cert) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(c.SelfSigned, c.AcmArn, c.PemBundle),
		constraint.AtLeastOneOf(c.SelfSigned, c.AcmArn),
		constraint.AtMostOneOf(c.AcmArn, c.PemBundle),
		constraint.RequiredTogether(c.PemBundle, c.PrivateKey),
		constraint.RequiredWith(c.PemBundle, c.PrivateKey),
		constraint.ForbiddenWith(c.AcmArn, c.RenewBefore),
	}
}

type CertOutput struct {
	ARN string
}

func (c *Cert) SchemaVersion() int { return 1 }

func (c *Cert) Create(_ context.Context, _ any) (*CertOutput, error) { return nil, nil }

func (c *Cert) Read(_ context.Context, _ any, _ *CertOutput) (*CertOutput, error) {
	return nil, nil
}

func (c *Cert) Update(_ context.Context, _ any, _ *CertOutput) (*CertOutput, error) {
	return nil, nil
}

func (c *Cert) Delete(_ context.Context, _ any, _ *CertOutput) error { return nil }
