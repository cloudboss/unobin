package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

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
