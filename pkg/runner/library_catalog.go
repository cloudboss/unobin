package runner

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func linkRunnerLibraries(info Info) (Info, error) {
	if info.LibraryRegistrations == nil && info.LibraryBindings == nil {
		if info.libraryCatalog == nil && len(info.Libraries) == 0 {
			catalog, err := runtime.NewLibraryCatalog(nil)
			info.libraryCatalog = catalog
			return info, err
		}
		return info, nil
	}
	if info.Libraries != nil {
		return info, fmt.Errorf("factory declares both libraries and catalog registrations")
	}
	catalog, err := runtime.NewLibraryCatalog(info.LibraryRegistrations)
	if err != nil {
		return info, err
	}
	libraries, err := catalog.Libraries(info.LibraryBindings)
	if err != nil {
		return info, err
	}
	info.libraryCatalog = catalog
	info.Libraries = libraries
	return info, nil
}
