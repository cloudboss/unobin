package syntax

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/stretchr/testify/require"
)

func TestValidateFactoryBodyTypedMatchesCurrentFixtures(t *testing.T) {
	fixtureRoots := []string{
		"testdata/ub/validate/valid/factory",
		"testdata/ub/validate/invalid/factory",
	}
	for _, root := range fixtureRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			require.NoError(t, err)
			if d.IsDir() || filepath.Ext(path) != ".ub" {
				return nil
			}
			t.Run(strings.TrimSuffix(path, ".ub"), func(t *testing.T) {
				src, err := os.ReadFile(path)
				require.NoError(t, err)
				f, err := ParseSource("factory.ub", src)
				if err != nil {
					require.Equal(t, factoryValidationGoldenText(t, path), strings.TrimSpace(err.Error()))
					return
				}
				require.NotNil(t, f.Factory)

				errs := parse.NewErrorList(0)
				validateFactoryBodyTyped(f.Factory.Body, errs)

				require.Equal(t, factoryValidationGolden(t, path), errs.Messages())
			})
			return nil
		})
		require.NoError(t, err)
	}
}

func factoryValidationGolden(t *testing.T, path string) []string {
	t.Helper()
	text := factoryValidationGoldenText(t, path)
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func factoryValidationGoldenText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path + ".err")
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		require.NoError(t, err)
	}
	return strings.TrimSpace(string(body))
}
