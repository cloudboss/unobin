package runtime

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/stateref"
)

type stepInstance struct {
	Address            string
	DeclarationAddress string
	keys               []keyPosition
	order              int
}

type stepInstances struct {
	instances    []stepInstance
	declarations map[string][]*stepInstance
	keyIndexes   map[instanceKeyIndexID]*instanceKeyIndex
}

type instanceKeyIndexID struct {
	declaration, position string
}

type instanceKeyIndex struct {
	values  map[string][]*stepInstance
	missing []*stepInstance
}

type stepCandidates struct {
	keyed, plain     []*stepInstance
	keyedAt, plainAt int
}

func indexStepInstances(addresses []string) *stepInstances {
	index := &stepInstances{
		instances:    make([]stepInstance, len(addresses)),
		declarations: make(map[string][]*stepInstance),
	}
	for i, address := range addresses {
		instance := &index.instances[i]
		instance.Address = address
		instance.DeclarationAddress, instance.keys = parseStepAddress(address)
		instance.order = i
		declaration := instance.DeclarationAddress
		index.declarations[declaration] = append(index.declarations[declaration], instance)
	}
	return index
}

func parseStepAddress(address string) (string, []keyPosition) {
	ref, err := stateref.ParseStateRef(address)
	if err != nil {
		return address, nil
	}
	var declaration strings.Builder
	declaration.Grow(len(address))
	var keys []keyPosition
	for i, segment := range ref.Segments {
		if i != 0 {
			declaration.WriteByte('/')
		}
		declaration.WriteString(string(segment.Category))
		declaration.WriteByte('.')
		declaration.WriteString(segment.Name)
		if segment.Key != nil {
			keys = append(keys, keyPosition{at: declaration.String(), key: segment.Key.Value})
		}
	}
	return declaration.String(), keys
}

func (index *stepInstances) candidates(
	declaration string, keys []keyPosition, narrow bool,
) stepCandidates {
	best := stepCandidates{keyed: index.declarations[declaration]}
	for _, position := range keys {
		if best.len() <= 1 {
			break
		}
		if declaration != position.at && !strings.HasPrefix(declaration, position.at+"/") {
			continue
		}
		byKey := index.keyIndex(declaration, position.at)
		candidate := stepCandidates{keyed: byKey.values[position.key], plain: byKey.missing}
		if candidate.len() < best.len() {
			best = candidate
		}
	}
	if narrow && best.len() > 1 {
		byKey := index.keyIndex(declaration, "")
		candidate := stepCandidates{keyed: byKey.values[keys[0].key]}
		if candidate.len() < best.len() {
			best = candidate
		}
	}
	return best
}

// An empty position matches a key at any level. Repeated keys add one candidate.
func (index *stepInstances) keyIndex(declaration, position string) *instanceKeyIndex {
	id := instanceKeyIndexID{declaration: declaration, position: position}
	if cached := index.keyIndexes[id]; cached != nil {
		return cached
	}
	byKey := &instanceKeyIndex{values: make(map[string][]*stepInstance)}
	for _, instance := range index.declarations[declaration] {
		found := false
		for i, key := range instance.keys {
			if position == "" {
				duplicate := false
				for _, previous := range instance.keys[:i] {
					if previous.key == key.key {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
			} else if key.at != position {
				continue
			}
			byKey.values[key.key] = append(byKey.values[key.key], instance)
			found = true
		}
		if !found && position != "" {
			byKey.missing = append(byKey.missing, instance)
		}
	}
	if index.keyIndexes == nil {
		index.keyIndexes = make(map[instanceKeyIndexID]*instanceKeyIndex)
	}
	index.keyIndexes[id] = byKey
	return byKey
}

func (c stepCandidates) len() int {
	return len(c.keyed) + len(c.plain)
}

func (c *stepCandidates) next() *stepInstance {
	if c.keyedAt == len(c.keyed) && c.plainAt == len(c.plain) {
		return nil
	}
	if c.keyedAt == len(c.keyed) ||
		(c.plainAt < len(c.plain) && c.plain[c.plainAt].order < c.keyed[c.keyedAt].order) {
		instance := c.plain[c.plainAt]
		c.plainAt++
		return instance
	}
	instance := c.keyed[c.keyedAt]
	c.keyedAt++
	return instance
}
