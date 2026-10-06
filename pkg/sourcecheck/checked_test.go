package sourcecheck

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestAnalyzeFactoryBodyRetainsLocalTypes(t *testing.T) {
	for _, name := range []string{"valid/local-types/factory", "invalid/local-types/factory"} {
		t.Run(name, func(t *testing.T) {
			body := parseFactoryAt(t, fixturePath(name))
			analysis, err := AnalyzeFactoryBody(body, Options{})
			if name == "invalid/local-types/factory" {
				requireErrorMatchesGolden(t, name, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, analysis)
			require.NotNil(t, analysis.Imports)
			require.Equal(t, map[string]typecheck.Type{
				"name": typecheck.TString(), "count": typecheck.TInteger(),
			}, analysis.LocalTypes)
			if err != nil {
				result, checkErr := CheckFactoryBody(body, Options{})
				require.Nil(t, result)
				require.EqualError(t, checkErr, err.Error())
			}
		})
	}
}

func TestAnalyzeFactoryBodyRejectsUnresolvedImport(t *testing.T) {
	body := parseFactoryAt(t, fixturePath("valid/no-fetch-missing-remote/factory"))
	analysis, err := AnalyzeFactoryBody(body, Options{})
	require.EqualError(t, err, "sourcecheck: resolver is required when dependencies are present")
	require.Nil(t, analysis)
}
