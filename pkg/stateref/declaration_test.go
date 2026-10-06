package stateref

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeclarationAddress(t *testing.T) {
	for _, test := range []struct {
		address string
		want    string
	}{
		{address: "resource.server", want: "resource.server"},
		{address: "resource.server['east']", want: "resource.server"},
		{address: "resource.group['east']/resource.server['a']",
			want: "resource.group/resource.server"},
		{address: "action.group['a/b']/data-source.current['c']",
			want: "action.group/data-source.current"},
	} {
		got, err := DeclarationAddress(test.address)
		require.NoError(t, err)
		require.Equal(t, test.want, got)
		legacy, err := Template(test.address)
		require.NoError(t, err)
		require.Equal(t, got, legacy)
	}
	_, err := DeclarationAddress("resource.group[")
	require.Error(t, err)
}
