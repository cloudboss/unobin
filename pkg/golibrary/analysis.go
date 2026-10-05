package golibrary

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
)

type analysisSourceRevision struct {
	roots  []ModuleSource
	digest [32]byte
	err    error
}

type analysisPackageCheck struct {
	linked bool
	err    error
}

type compatibilityAnalysis struct {
	sources map[string]analysisSourceRevision
	checks  map[string]analysisPackageCheck
	walker  *configurationWalker
}

// ForAnalysis returns an independent context for one import analysis.
// Call ValidateSources before using the result and EndAnalysis before later reads.
func (c *CompatibilityContext) ForAnalysis() *CompatibilityContext {
	clone := *c
	clone.options.Modules = slices.Clone(c.options.Modules)
	clone.entries = map[string]PackageMetadata{}
	clone.analysis = &compatibilityAnalysis{
		sources: map[string]analysisSourceRevision{}, checks: map[string]analysisPackageCheck{},
	}
	return &clone
}

// ValidateSources checks the source revisions used by this analysis.
func (c *CompatibilityContext) ValidateSources() error {
	if c.analysis == nil || len(c.analysis.sources) == 0 {
		return nil
	}
	if err := c.checkCoreReplacement(); err != nil {
		return err
	}
	for _, dir := range slices.Sorted(maps.Keys(c.analysis.sources)) {
		source := c.analysis.sources[dir]
		if source.err != nil {
			return source.err
		}
		digest, err := SourceSnapshot(dir, source.roots)
		if err != nil || digest != source.digest {
			return c.changedSource(dir, err)
		}
	}
	return nil
}

// EndAnalysis restores current-source checks for subsequent reads.
func (c *CompatibilityContext) EndAnalysis() {
	c.analysis = nil
}

func (c *CompatibilityContext) analysisSnapshot(dir string) ([32]byte, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return [32]byte{}, err
	}
	roots := c.ModuleSources()
	source, found := c.analysis.sources[abs]
	if found && slices.Equal(source.roots, roots) {
		return source.digest, source.err
	}
	if found {
		if source.err != nil {
			return [32]byte{}, source.err
		}
		digest, err := SourceSnapshot(abs, source.roots)
		if err != nil || digest != source.digest {
			return [32]byte{}, c.changedSource(abs, err)
		}
	}
	digest, err := SourceSnapshot(abs, roots)
	c.analysis.sources[abs] = analysisSourceRevision{roots: roots, digest: digest, err: err}
	return digest, err
}

func (c *CompatibilityContext) changedSource(dir string, cause error) error {
	fresh := *c
	fresh.analysis = nil
	fresh.entries = map[string]PackageMetadata{}
	fresh.options.Modules = slices.Clone(c.options.Modules)
	if err := fresh.CheckDirectory(dir, c.entries[dir].Source.Linked); err != nil {
		return err
	}
	if cause != nil {
		return fmt.Errorf("library source changed during import analysis: %s: %w", dir, cause)
	}
	return fmt.Errorf("library source changed during import analysis: %s", dir)
}
