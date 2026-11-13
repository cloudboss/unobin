package runtime

func librariesForNode(dag *DAG, root map[string]*Library, n *Node) map[string]*Library {
	if n == nil || n.Composite == "" {
		return root
	}
	if dag != nil {
		if boundary := dag.Nodes[declarationAddress(n.Composite)]; boundary != nil {
			return boundary.Libraries
		}
	}
	return nil
}

func (e *Executor) newEvalScope(
	boundary *Node, inputs map[string]any,
) (*EvalContext, error) {
	setID := e.RootAssetSetID
	libraries := e.Libraries
	locals := e.rootLocalExprs()
	if boundary != nil {
		setID = boundary.AssetSetID
		libraries = boundary.Libraries
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
