package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeVpc struct {
	CidrBlock string
}

type fakeVpcOutput struct {
	ID string
}

func (v *fakeVpc) SchemaVersion() int { return 1 }

func (v *fakeVpc) Create(_ context.Context, _ any) (*fakeVpcOutput, error) {
	return &fakeVpcOutput{ID: "vpc-" + v.CidrBlock}, nil
}

func (v *fakeVpc) Read(_ context.Context, _ any, prior *fakeVpcOutput) (*fakeVpcOutput, error) {
	return prior, nil
}

func (v *fakeVpc) Update(
	ctx context.Context, cfg any, _ Prior[fakeVpc, *fakeVpcOutput],
) (*fakeVpcOutput, error) {
	return v.Create(ctx, cfg)
}

func (v *fakeVpc) Delete(_ context.Context, _ any, _ *fakeVpcOutput) error {
	return nil
}

func (v *fakeVpc) ReplaceFields() []string { return []string{"cidr-block"} }

func TestMakeResourceProducesWorkingRegistration(t *testing.T) {
	reg := MakeResource[fakeVpc, *fakeVpcOutput, any]()
	require.Equal(t, 1, reg.SchemaVersion())

	receiver := reg.NewReceiver()
	vpc, ok := receiver.(*fakeVpc)
	require.True(t, ok, "NewReceiver should return *fakeVpc, got %T", receiver)
	require.NotNil(t, vpc)

	require.NoError(t, Decode(receiver, map[string]any{"cidr-block": "10.0.0.0/16"}))

	result, err := reg.Create(context.Background(), receiver, nil)
	require.NoError(t, err)
	out, ok := result.(*fakeVpcOutput)
	require.True(t, ok, "Create should return *fakeVpcOutput, got %T", result)
	require.Equal(t, "vpc-10.0.0.0/16", out.ID)

	readBack, err := reg.Read(context.Background(), receiver, nil, out)
	require.NoError(t, err)
	require.Equal(t, out, readBack)

	require.Equal(t, []string{"cidr-block"}, reg.ReplaceFields(receiver))
	require.Equal(t, reflect.TypeFor[*fakeVpcOutput](), reg.OutputType())
}

func TestResourceMigrateErrorsWhenNoMigratorImplemented(t *testing.T) {
	reg := MakeResource[fakeVpc, *fakeVpcOutput, any]()
	_, err := reg.Migrate(0, MigrationState{Outputs: map[string]any{"old": "state"}})
	require.Error(t, err)
}

type migratingVpc struct {
	fakeVpc
}

func (v *migratingVpc) Update(
	ctx context.Context, cfg any, _ Prior[migratingVpc, *fakeVpcOutput],
) (*fakeVpcOutput, error) {
	return v.Create(ctx, cfg)
}

func (v *migratingVpc) Migrate(old int, prior MigrationState) (MigrationState, error) {
	prior.Inputs["migrated-from-version"] = old
	prior.Outputs["migrated-from-version"] = old
	return prior, nil
}

func TestResourceMigrateCallsMigratorWhenImplemented(t *testing.T) {
	reg := MakeResource[migratingVpc, *fakeVpcOutput, any]()
	out, err := reg.Migrate(0, MigrationState{
		Inputs:  map[string]any{"cidr-block": "10.0.0.0/8"},
		Outputs: map[string]any{"original": "value"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, out.Inputs["migrated-from-version"])
	require.Equal(t, "10.0.0.0/8", out.Inputs["cidr-block"])
	require.Equal(t, 0, out.Outputs["migrated-from-version"])
	require.Equal(t, "value", out.Outputs["original"])
}

// capturingVpc records the Prior its Update receives so a test can
// assert the bundle holds both prior inputs and prior outputs.
type capturingVpc struct {
	CidrBlock string
	gotPrior  *Prior[capturingVpc, *fakeVpcOutput]
}

func (v *capturingVpc) SchemaVersion() int { return 1 }

func (v *capturingVpc) Create(_ context.Context, _ any) (*fakeVpcOutput, error) {
	return &fakeVpcOutput{ID: "vpc-" + v.CidrBlock}, nil
}

func (v *capturingVpc) Read(
	_ context.Context, _ any, prior *fakeVpcOutput,
) (*fakeVpcOutput, error) {
	return prior, nil
}

func (v *capturingVpc) Update(
	_ context.Context, _ any, prior Prior[capturingVpc, *fakeVpcOutput],
) (*fakeVpcOutput, error) {
	v.gotPrior = &prior
	return prior.Outputs, nil
}

func (v *capturingVpc) Delete(_ context.Context, _ any, _ *fakeVpcOutput) error { return nil }

func (v *capturingVpc) ReplaceFields() []string { return nil }

type typedFakeAction struct {
	Argv []string
}

type typedFakeActionOutput struct {
	Stdout string
}

func (a *typedFakeAction) Run(_ context.Context, _ any) (*typedFakeActionOutput, error) {
	return &typedFakeActionOutput{Stdout: "ran: " + a.Argv[0]}, nil
}

func TestMakeActionProducesWorkingRegistration(t *testing.T) {
	reg := MakeAction[typedFakeAction, *typedFakeActionOutput, any]()

	receiver := reg.NewReceiver()
	require.NoError(t, Decode(receiver, map[string]any{"argv": []any{"echo"}}))

	result, err := reg.Run(context.Background(), receiver, nil)
	require.NoError(t, err)
	out, ok := result.(*typedFakeActionOutput)
	require.True(t, ok)
	require.Equal(t, "ran: echo", out.Stdout)
	require.Equal(t, reflect.TypeFor[*typedFakeActionOutput](), reg.OutputType())
}

type fakeAMI struct {
	ImageID string
}

type fakeAMIOutput struct {
	Architecture string
}

func (d *fakeAMI) Read(_ context.Context, _ any) (*fakeAMIOutput, error) {
	return &fakeAMIOutput{Architecture: "x86_64"}, nil
}

func TestCoercePriorNil(t *testing.T) {
	got, err := coercePrior[*fakeVpcOutput](nil)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCoercePriorPassesTypedValueThrough(t *testing.T) {
	prior := &fakeVpcOutput{ID: "vpc-abc"}
	got, err := coercePrior[*fakeVpcOutput](prior)
	require.NoError(t, err)
	require.Equal(t, prior, got)
}

func TestCoercePriorFromStateMap(t *testing.T) {
	// State on disk is JSON-decoded, so prior outputs arrive as
	// map[string]any rather than the typed *Out. coercePrior must
	// decode the map into the typed output.
	prior := map[string]any{"id": "vpc-abc"}
	got, err := coercePrior[*fakeVpcOutput](prior)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "vpc-abc", got.ID)
}

func TestCoercePriorUnsupportedTypeReturnsError(t *testing.T) {
	// A prior that is neither nil, the typed output, nor a map cannot
	// be coerced. It must return an error, not crash.
	_, err := coercePrior[*fakeVpcOutput](42)
	require.Error(t, err)
}

func TestCoercePriorUndecodableMapReturnsError(t *testing.T) {
	// A state map with a field the output struct does not declare fails
	// to decode. It must return an error, not crash.
	_, err := coercePrior[*fakeVpcOutput](map[string]any{"unknown-field": "x"})
	require.Error(t, err)
}

func TestCoercePriorInputsNilYieldsZero(t *testing.T) {
	require.Equal(t, fakeVpc{}, coercePriorInputs[fakeVpc](nil))
}

func TestCoercePriorInputsDecodesStateMap(t *testing.T) {
	got := coercePriorInputs[fakeVpc](map[string]any{"cidr-block": "10.0.0.0/16"})
	require.Equal(t, "10.0.0.0/16", got.CidrBlock)
}

func TestCoercePriorInputsUndecodableMapYieldsZero(t *testing.T) {
	// Prior inputs are advisory: a field that no longer decodes (removed
	// or retyped since the last apply) degrades to the zero value, which
	// reads as "every field changed", rather than failing the apply the
	// way an undecodable prior output does.
	require.Equal(t, fakeVpc{}, coercePriorInputs[fakeVpc](map[string]any{"gone": "x"}))
}

func TestCoercePriorInputsUnsupportedTypeYieldsZero(t *testing.T) {
	require.Equal(t, fakeVpc{}, coercePriorInputs[fakeVpc](42))
}

func TestUpdateReceivesPriorInputsOutputsAndObserved(t *testing.T) {
	// The erased Update builds the Prior bundle from the prior inputs, the
	// prior outputs, and the plan-time observed outputs the runtime hands
	// it, all arriving as state maps. Outputs is the recorded handle;
	// Observed is what the plan-time Read saw, which can differ under drift.
	reg := MakeResource[capturingVpc, *fakeVpcOutput, any]()
	receiver := &capturingVpc{CidrBlock: "10.0.0.0/16"}
	_, err := reg.Update(context.Background(), receiver, nil,
		map[string]any{"cidr-block": "10.0.0.0/8"},
		map[string]any{"id": "vpc-recorded"},
		map[string]any{"id": "vpc-observed"})
	require.NoError(t, err)
	require.NotNil(t, receiver.gotPrior)
	require.Equal(t, "10.0.0.0/8", receiver.gotPrior.Inputs.CidrBlock)
	require.Equal(t, "vpc-recorded", receiver.gotPrior.Outputs.ID)
	require.Equal(t, "vpc-observed", receiver.gotPrior.Observed.ID)
}

func TestUpdatePassesNilObservedAsZero(t *testing.T) {
	// A resource with no plan-time read (or a nil observed map) gets the
	// zero Out, the same nil-pointer convention Outputs uses.
	reg := MakeResource[capturingVpc, *fakeVpcOutput, any]()
	receiver := &capturingVpc{CidrBlock: "10.0.0.0/16"}
	_, err := reg.Update(context.Background(), receiver, nil,
		map[string]any{"cidr-block": "10.0.0.0/8"}, map[string]any{"id": "vpc-old"}, nil)
	require.NoError(t, err)
	require.NotNil(t, receiver.gotPrior)
	require.Nil(t, receiver.gotPrior.Observed)
}

func TestChanged(t *testing.T) {
	a, b := "x", "x"
	c := "y"
	tests := []struct {
		name           string
		prior, current any
		want           bool
	}{
		{"equal values", "x", "x", false},
		{"different values", "x", "y", true},
		{"equal ints", 5, 5, false},
		{"pointers to equal values compare by value", &a, &b, false},
		{"pointers to different values", &a, &c, true},
		{"nil pointer vs set pointer", (*string)(nil), &a, true},
		{"equal maps", map[string]string{"k": "v"}, map[string]string{"k": "v"}, false},
		{"different maps", map[string]string{"k": "v"}, map[string]string{"k": "w"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Changed(tt.prior, tt.current))
		})
	}
}

func TestReadAcceptsStateMapPrior(t *testing.T) {
	// State on disk is JSON-decoded, so typed Read is called with the
	// map[string]any prior.
	reg := MakeResource[fakeVpc, *fakeVpcOutput, any]()
	receiver := reg.NewReceiver()
	prior := map[string]any{"id": "vpc-abc"}
	out, err := reg.Read(context.Background(), receiver, nil, prior)
	require.NoError(t, err)
	require.Equal(t, "vpc-abc", out.(*fakeVpcOutput).ID)
}

func TestReadReturnsErrorOnUndecodableStateMap(t *testing.T) {
	// A corrupt prior state entry is reported as a Read error rather
	// than a panic out of the registration.
	reg := MakeResource[fakeVpc, *fakeVpcOutput, any]()
	receiver := reg.NewReceiver()
	prior := map[string]any{"unknown-field": "x"}
	_, err := reg.Read(context.Background(), receiver, nil, prior)
	require.Error(t, err)
}

func TestMakeDataSourceProducesWorkingRegistration(t *testing.T) {
	reg := MakeDataSource[fakeAMI, *fakeAMIOutput, any]()

	receiver := reg.NewReceiver()
	require.NoError(t, Decode(receiver, map[string]any{"image-id": "ami-123"}))

	result, err := reg.Read(context.Background(), receiver, nil)
	require.NoError(t, err)
	out, ok := result.(*fakeAMIOutput)
	require.True(t, ok)
	require.Equal(t, "x86_64", out.Architecture)
	require.Equal(t, reflect.TypeFor[*fakeAMIOutput](), reg.OutputType())
}
