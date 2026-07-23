package runner

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	assetCacheDir string
	assets        *runnerAssets
}

type runnerAssets struct {
	catalog        *asset.Catalog
	cache          *asset.Cache
	rootAssetSetID string
}

func loadRunnerAssets(info Info, requestedRoot string) (*runnerAssets, error) {
	var catalog *asset.Catalog
	if len(info.AssetBundle) > 0 {
		var err error
		catalog, err = asset.Open(info.AssetBundle)
		if err != nil {
			return nil, err
		}
	}
	if err := checkAssetSet(catalog, info.RootAssetSetID, ""); err != nil {
		return nil, err
	}
	if info.FactoryBody != nil {
		dag := runtime.BuildSyntaxDAG(*info.FactoryBody, info.Libraries)
		for _, address := range slices.Sorted(maps.Keys(dag.Nodes)) {
			node := dag.Nodes[address]
			if !node.IsComposite() {
				continue
			}
			if err := checkAssetSet(catalog, node.AssetSetID, address); err != nil {
				return nil, err
			}
		}
	}
	cache, err := asset.NewCache(catalog, requestedRoot)
	if err != nil {
		return nil, err
	}
	cache.HideRootInErrors()
	return &runnerAssets{
		catalog:        catalog,
		cache:          cache,
		rootAssetSetID: info.RootAssetSetID,
	}, nil
}

func checkRunnerAssets(command *cobra.Command, info Info) error {
	loaded, err := loadRunnerAssets(info, info.options.assetCacheDir)
	if err == nil {
		info.options.assets = loaded
		return nil
	}
	startupErr := errors.New(err.Error())
	format := cmdout.FormatText
	if command.Flags().Lookup("format") != nil {
		format, err = runnerCommandFormat(command)
		if err != nil {
			return err
		}
	}
	return commandResultFailure(command, format, nil, startupErr)
}

func runnerCommandFormat(command *cobra.Command) (cmdout.Format, error) {
	formatFlag := command.Flags().Lookup("format")
	outputFlag := command.Flags().Lookup("output")
	if outputFlag != nil &&
		command.Flags().Changed("output") &&
		(formatFlag == nil || !command.Flags().Changed("format")) {
		value, err := command.Flags().GetString("output")
		if err != nil {
			return "", err
		}
		format, err := ParseFormat(value)
		return cmdout.Format(format), err
	}
	return commandFormat(command)
}

func checkAssetSet(catalog *asset.Catalog, id, address string) error {
	if id == "" {
		return nil
	}
	if _, ok := catalog.Set(id); ok {
		return nil
	}
	if address == "" {
		return fmt.Errorf("asset set %q: not found in asset bundle", id)
	}
	return fmt.Errorf("asset set %q for composite %s: not found in asset bundle", id, address)
}

func runnerAssetsFor(info Info) (*runnerAssets, error) {
	if info.options != nil && info.options.assets != nil {
		return info.options.assets, nil
	}
	requestedRoot := ""
	if info.options != nil {
		requestedRoot = info.options.assetCacheDir
	}
	loaded, err := loadRunnerAssets(info, requestedRoot)
	if err != nil {
		return nil, err
	}
	if info.options != nil {
		info.options.assets = loaded
	}
	return loaded, nil
}

func (a *runnerAssets) configureExecutor(exec *runtime.Executor) {
	if a == nil || exec == nil {
		return
	}
	exec.AssetCatalog = a.catalog
	exec.AssetCache = a.cache
	exec.RootAssetSetID = a.rootAssetSetID
}
