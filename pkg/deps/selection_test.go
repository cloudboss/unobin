package deps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectionAddSequence(t *testing.T) {
	dep := Dependency{URL: "github.com/x/y"}
	s := NewSelection()
	steps := []struct {
		floor      string
		wantRaised bool
		wantVer    string
	}{
		{"v1.2.0", true, "v1.2.0"},      // first sighting always raises
		{"v1.1.0", false, "v1.2.0"},     // a lower floor changes nothing
		{"v1.2.0", false, "v1.2.0"},     // an equal floor changes nothing
		{"v1.3.0", true, "v1.3.0"},      // a higher floor raises
		{"v1.3.0-rc1", false, "v1.3.0"}, // a prerelease is below the release
		{"v2.0.0", true, "v2.0.0"},      // a major bump raises
	}
	for _, step := range steps {
		t.Run(step.floor, func(t *testing.T) {
			assert.Equal(t, step.wantRaised, s.Add(dep, step.floor))
			assert.Equal(t, step.wantVer, s.Version(dep))
		})
	}
}

func TestSelectionTracksDepsIndependently(t *testing.T) {
	a := Dependency{URL: "github.com/x/a"}
	b := Dependency{URL: "github.com/x/b", Subdir: "sub"}
	s := NewSelection()
	s.Add(a, "v1.0.0")
	s.Add(b, "v2.0.0")
	s.Add(a, "v1.5.0")
	assert.Equal(t, map[Dependency]string{a: "v1.5.0", b: "v2.0.0"}, s.Chosen())
}

func TestSelectionIsOrderIndependent(t *testing.T) {
	a := Dependency{URL: "github.com/x/a"}
	b := Dependency{URL: "github.com/x/b"}
	want := map[Dependency]string{a: "v1.2.0", b: "v3.0.0"}
	orders := [][]struct {
		dep   Dependency
		floor string
	}{
		{{a, "v1.0.0"}, {a, "v1.2.0"}, {a, "v1.1.0"}, {b, "v3.0.0"}, {b, "v2.0.0"}},
		{{b, "v2.0.0"}, {a, "v1.1.0"}, {b, "v3.0.0"}, {a, "v1.2.0"}, {a, "v1.0.0"}},
		{{a, "v1.2.0"}, {b, "v3.0.0"}, {a, "v1.0.0"}, {b, "v2.0.0"}, {a, "v1.1.0"}},
	}
	for i, order := range orders {
		s := NewSelection()
		for _, r := range order {
			s.Add(r.dep, r.floor)
		}
		assert.Equalf(t, want, s.Chosen(), "order %d should select the max per dependency", i)
	}
}

func TestSelectionVersionUnknown(t *testing.T) {
	assert.Equal(t, "", NewSelection().Version(Dependency{URL: "github.com/x/y"}))
}

func TestSelectionChosenIsACopy(t *testing.T) {
	dep := Dependency{URL: "github.com/x/y"}
	s := NewSelection()
	s.Add(dep, "v1.0.0")
	c := s.Chosen()
	c[dep] = "v9.9.9"
	assert.Equal(t, "v1.0.0", s.Version(dep))
}
