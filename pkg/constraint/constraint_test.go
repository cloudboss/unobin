package constraint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// CertInput is a stand-in for a real library input type: the test builds a
// realistic mixed set of constraints against its fields, which both proves
// the field-selector form type-checks through the API and lets us read back
// the kind and message each builder records.
type CertInput struct {
	SelfSigned      *bool
	AcmArn          *string
	PemBundle       *string
	PrivateKey      *string
	Tier            string
	ValidityDays    *int
	RenewBeforeDays *int
	Region          string
}

func (v CertInput) Constraints() []Constraint {
	return []Constraint{
		ExactlyOneOf(v.SelfSigned, v.AcmArn, v.PemBundle),
		RequiredTogether(v.PemBundle, v.PrivateKey),
		Must(OneOf(v.Region, "us-east-1", "us-west-2")),
		When(Equals(v.Tier, "prod")).
			Require(Present(v.ValidityDays), AtLeast(v.ValidityDays, 90)).
			Message("production certs need a validity of at least 90 days"),
		Must(Below(v.RenewBeforeDays, v.ValidityDays)),
		When(All(Present(v.AcmArn), IsTrue(v.SelfSigned))).
			Require(Absent(v.RenewBeforeDays)).
			Message("ACM manages renewal; remove renew-before-days"),
	}
}

func TestConstraintsRecordKindAndMessage(t *testing.T) {
	got := CertInput{}.Constraints()

	want := []struct {
		kind    Kind
		message string
	}{
		{KindExactlyOneOf, ""},
		{KindRequiredTogether, ""},
		{KindPredicate, ""},
		{KindPredicate, "production certs need a validity of at least 90 days"},
		{KindPredicate, ""},
		{KindPredicate, "ACM manages renewal; remove renew-before-days"},
	}
	require.Len(t, got, len(want))
	for i, w := range want {
		require.Equal(t, w.kind, got[i].kind, "constraint %d kind", i)
		require.Equal(t, w.message, got[i].message, "constraint %d message", i)
	}
}

func TestEachBuilderRecordsItsKind(t *testing.T) {
	tests := []struct {
		name string
		c    Constraint
		want Kind
	}{
		{"exactly-one-of", ExactlyOneOf(nil, nil), KindExactlyOneOf},
		{"at-least-one-of", AtLeastOneOf(nil, nil), KindAtLeastOneOf},
		{"at-most-one-of", AtMostOneOf(nil, nil), KindAtMostOneOf},
		{"required-together", RequiredTogether(nil, nil), KindRequiredTogether},
		{"required-with", RequiredWith(nil, nil), KindRequiredWith},
		{"forbidden-with", ForbiddenWith(nil, nil), KindForbiddenWith},
		{"must", Must(Present(nil)), KindPredicate},
		{"when-require", When(Equals(nil, nil)).Require(Present(nil)), KindPredicate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.c.kind)
		})
	}
}

func TestMessageReturnsUpdatedCopy(t *testing.T) {
	base := Must(Present(struct{}{}))
	withMsg := base.Message("needed")
	require.Equal(t, "", base.message, "Message must not mutate the original")
	require.Equal(t, "needed", withMsg.message)
}
