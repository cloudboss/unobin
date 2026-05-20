package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeVpc struct {
	CidrBlock string `mapstructure:"cidr-block"`
}

type fakeVpcOutput struct {
	ID string `mapstructure:"id"`
}

func (v *fakeVpc) SchemaVersion() int { return 1 }

func (v *fakeVpc) Create(_ context.Context, _ any) (*fakeVpcOutput, error) {
	return &fakeVpcOutput{ID: "vpc-" + v.CidrBlock}, nil
}

func (v *fakeVpc) Read(_ context.Context, _ any, prior *fakeVpcOutput) (*fakeVpcOutput, error) {
	return prior, nil
}

func (v *fakeVpc) Update(ctx context.Context, cfg any, _ *fakeVpcOutput) (*fakeVpcOutput, error) {
	return v.Create(ctx, cfg)
}

func (v *fakeVpc) Delete(_ context.Context, _ any, _ *fakeVpcOutput) error {
	return nil
}

func (v *fakeVpc) ReplaceFields() []string { return []string{"cidr-block"} }

func TestMakeResourceProducesWorkingRegistration(t *testing.T) {
	reg := MakeResource[fakeVpc, *fakeVpcOutput]()
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
	require.Equal(t, reflect.TypeOf(&fakeVpcOutput{}), reg.OutputType())
}

func TestResourceMigrateErrorsWhenNoMigratorImplemented(t *testing.T) {
	reg := MakeResource[fakeVpc, *fakeVpcOutput]()
	_, err := reg.Migrate(0, map[string]any{"old": "state"})
	require.Error(t, err)
}

type migratingVpc struct {
	fakeVpc
}

func (v *migratingVpc) Migrate(old int, oldState map[string]any) (map[string]any, error) {
	oldState["migrated-from-version"] = old
	return oldState, nil
}

func TestResourceMigrateCallsMigratorWhenImplemented(t *testing.T) {
	reg := MakeResource[migratingVpc, *fakeVpcOutput]()
	out, err := reg.Migrate(0, map[string]any{"original": "value"})
	require.NoError(t, err)
	require.Equal(t, 0, out["migrated-from-version"])
	require.Equal(t, "value", out["original"])
}

type typedFakeAction struct {
	Argv []string `mapstructure:"argv"`
}

type typedFakeActionOutput struct {
	Stdout string `mapstructure:"stdout"`
}

func (a *typedFakeAction) Run(_ context.Context, _ any) (*typedFakeActionOutput, error) {
	return &typedFakeActionOutput{Stdout: "ran: " + a.Argv[0]}, nil
}

func TestMakeActionProducesWorkingRegistration(t *testing.T) {
	reg := MakeAction[typedFakeAction, *typedFakeActionOutput]()

	receiver := reg.NewReceiver()
	require.NoError(t, Decode(receiver, map[string]any{"argv": []any{"echo"}}))

	result, err := reg.Run(context.Background(), receiver, nil)
	require.NoError(t, err)
	out, ok := result.(*typedFakeActionOutput)
	require.True(t, ok)
	require.Equal(t, "ran: echo", out.Stdout)
	require.Equal(t, reflect.TypeOf(&typedFakeActionOutput{}), reg.OutputType())
}

type fakeAMI struct {
	ImageID string `mapstructure:"image-id"`
}

type fakeAMIOutput struct {
	Architecture string `mapstructure:"architecture"`
}

func (d *fakeAMI) Read(_ context.Context, _ any) (*fakeAMIOutput, error) {
	return &fakeAMIOutput{Architecture: "x86_64"}, nil
}

func TestCoercePriorNil(t *testing.T) {
	got := coercePrior[*fakeVpcOutput](nil)
	require.Nil(t, got)
}

func TestCoercePriorPassesTypedValueThrough(t *testing.T) {
	prior := &fakeVpcOutput{ID: "vpc-abc"}
	got := coercePrior[*fakeVpcOutput](prior)
	require.Equal(t, prior, got)
}

func TestCoercePriorFromStateMap(t *testing.T) {
	// State on disk is JSON-decoded, so prior outputs arrive as
	// map[string]any rather than the typed *Out. coercePrior must
	// decode the map into the typed shape.
	prior := map[string]any{"id": "vpc-abc"}
	got := coercePrior[*fakeVpcOutput](prior)
	require.NotNil(t, got)
	require.Equal(t, "vpc-abc", got.ID)
}

func TestReadAcceptsStateMapPrior(t *testing.T) {
	// The plan code path that panicked: typed Read called with the
	// JSON-decoded prior.
	reg := MakeResource[fakeVpc, *fakeVpcOutput]()
	receiver := reg.NewReceiver()
	prior := map[string]any{"id": "vpc-abc"}
	out, err := reg.Read(context.Background(), receiver, nil, prior)
	require.NoError(t, err)
	require.Equal(t, "vpc-abc", out.(*fakeVpcOutput).ID)
}

func TestMakeDataSourceProducesWorkingRegistration(t *testing.T) {
	reg := MakeDataSource[fakeAMI, *fakeAMIOutput]()

	receiver := reg.NewReceiver()
	require.NoError(t, Decode(receiver, map[string]any{"image-id": "ami-123"}))

	result, err := reg.Read(context.Background(), receiver, nil)
	require.NoError(t, err)
	out, ok := result.(*fakeAMIOutput)
	require.True(t, ok)
	require.Equal(t, "x86_64", out.Architecture)
	require.Equal(t, reflect.TypeOf(&fakeAMIOutput{}), reg.OutputType())
}
