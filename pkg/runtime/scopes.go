package runtime

func (e *Executor) newEvalScope(
	boundary *Node, inputs map[string]any,
) (*EvalContext, error) {
	setID := e.RootAssetSetID
	libraries := e.Libraries
	locals := e.rootLocalExprs()
	if boundary != nil {
		setID = boundary.AssetSetID
		libraries = compositeBodyLibraries(boundary, e.Libraries)
		locals = compositeLocalExprs(boundary)
	}
	assets, err := e.assetSet(setID)
	if err != nil {
		return nil, err
	}
	return &EvalContext{
		Inputs:     inputs,
		Resources:  make(map[string]any),
		Data:       make(map[string]any),
		Actions:    make(map[string]any),
		Libraries:  libraries,
		Assets:     assets,
		AssetCache: e.AssetCache,
		locals:     newLocalScopeFromMap(locals),
	}, nil
}
