package deps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectionAddSequence(t *testing.T) {
	d := Dependency{URL: "github.com/x/y"}
	s := NewSelection()
	steps := []struct {
		floor      string
		wantRaised bool
		wantVer    string
	}{
		{"v1.2.0", true, "v1.2.0"},      // first sighting always raises
		{"v1.1.0", false, "v1.2.0"},     // a lower floor changes nothing
		{"v1.2.0", false, "v1.2.0"},     // an equal floor changes nothing
		{"v1.2.5", true, "v1.2.5"},      // a higher patch raises
		{"v1.3.0", true, "v1.3.0"},      // a higher minor raises
		{"v1.3.0-rc1", false, "v1.3.0"}, // a prerelease is below the release
		{"v1.10.0", true, "v1.10.0"},    // double-digit minor sorts above v1.3
		{"v2.0.0", true, "v2.0.0"},      // a major bump raises
		{"v1.99.0", false, "v2.0.0"},    // nothing below the current max raises
	}
	for _, step := range steps {
		t.Run(step.floor, func(t *testing.T) {
			assert.Equal(t, step.wantRaised, s.Add(d, step.floor))
			assert.Equal(t, step.wantVer, s.Version(d))
		})
	}
}

func TestSelectionPicksHigher(t *testing.T) {
	cases := []struct {
		name   string
		lo, hi string
	}{
		{"patch", "v1.0.0", "v1.0.1"},
		{"minor", "v1.0.0", "v1.1.0"},
		{"major", "v1.9.9", "v2.0.0"},
		{"double-digit minor", "v1.9.0", "v1.10.0"},
		{"double-digit patch", "v1.0.9", "v1.0.10"},
		{"prerelease below release", "v1.0.0-rc1", "v1.0.0"},
		{"prerelease alphabetical", "v1.0.0-alpha", "v1.0.0-beta"},
		{"prerelease numeric not lexical", "v1.0.0-rc.2", "v1.0.0-rc.10"},
		{"v0 minor", "v0.1.0", "v0.2.0"},
		{"v0 patch", "v0.0.1", "v0.0.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Dependency{URL: "github.com/x/y"}

			loFirst := NewSelection()
			loFirst.Add(d, c.lo)
			loFirst.Add(d, c.hi)
			assert.Equal(t, c.hi, loFirst.Version(d), "adding low then high")

			hiFirst := NewSelection()
			hiFirst.Add(d, c.hi)
			hiFirst.Add(d, c.lo)
			assert.Equal(t, c.hi, hiFirst.Version(d), "adding high then low")
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
	c := Dependency{URL: "github.com/x/c"}
	want := map[Dependency]string{a: "v1.2.0", b: "v3.0.0", c: "v0.5.0"}
	type req struct {
		dep   Dependency
		floor string
	}
	orders := [][]req{
		{{a, "v1.0.0"}, {a, "v1.2.0"}, {a, "v1.1.0"}, {b, "v3.0.0"}, {b, "v2.0.0"}, {c, "v0.5.0"}, {c, "v0.4.0"}},
		{{c, "v0.4.0"}, {b, "v2.0.0"}, {a, "v1.1.0"}, {b, "v3.0.0"}, {a, "v1.2.0"}, {c, "v0.5.0"}, {a, "v1.0.0"}},
		{{a, "v1.2.0"}, {b, "v3.0.0"}, {c, "v0.5.0"}, {a, "v1.1.0"}, {b, "v2.0.0"}, {c, "v0.4.0"}, {a, "v1.0.0"}},
		{{c, "v0.5.0"}, {c, "v0.4.0"}, {b, "v3.0.0"}, {b, "v2.0.0"}, {a, "v1.2.0"}, {a, "v1.1.0"}, {a, "v1.0.0"}},
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
