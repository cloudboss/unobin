package runtime

type compositeOrder struct {
	internals map[string][]*Node
	owners    map[string]string
}

func buildCompositeOrder(nodes map[string]*Node, order []string) *compositeOrder {
	index := &compositeOrder{
		internals: map[string][]*Node{}, owners: map[string]string{},
	}
	var owner func(string) string
	owner = func(address string) string {
		if address == "" {
			return ""
		}
		if cached, ok := index.owners[address]; ok {
			return cached
		}
		var parent string
		if boundary := nodes[address]; boundary != nil {
			if boundary.IsComposite() && boundary.ForEach != nil {
				parent = address
			} else {
				parent = owner(boundary.Composite)
			}
		}
		index.owners[address] = parent
		return parent
	}
	counts := map[string]int{}
	total := 0
	for _, address := range order {
		node := nodes[address]
		if node == nil {
			continue
		}
		for parent := owner(node.Composite); parent != ""; parent = owner(nodes[parent].Composite) {
			counts[parent]++
			total++
		}
	}
	storage := make([]*Node, total)
	index.internals = make(map[string][]*Node, len(counts))
	for parent, count := range counts {
		index.internals[parent] = storage[:0:count]
		storage = storage[count:]
	}
	for _, address := range order {
		node := nodes[address]
		if node == nil {
			continue
		}
		for parent := owner(node.Composite); parent != ""; parent = owner(nodes[parent].Composite) {
			index.internals[parent] = append(index.internals[parent], node)
		}
	}
	return index
}
