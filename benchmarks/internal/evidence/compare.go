package evidence

import (
	"cmp"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"golang.org/x/perf/benchmath"
)

func Compare(
	before Record,
	beforeFiles map[string][]byte,
	after Record,
	afterFiles map[string][]byte,
) (Summary, error) {
	left, err := Validate(before, beforeFiles)
	if err != nil {
		return Summary{}, fmt.Errorf("before: %w", err)
	}
	right, err := Validate(after, afterFiles)
	if err != nil {
		return Summary{}, fmt.Errorf("after: %w", err)
	}
	if !reflect.DeepEqual(before.Spec, after.Spec) ||
		before.FixtureRevision != after.FixtureRevision || !maps.Equal(before.Fixtures, after.Fixtures) {
		return Summary{}, fmt.Errorf("benchmark definitions or fixture content differ")
	}
	if before.Machine != after.Machine || !maps.Equal(before.Dependencies, after.Dependencies) ||
		before.Repository != after.Repository {
		return Summary{}, fmt.Errorf("toolchain, machine, source path, or selected dependencies differ")
	}
	leftEnv, rightEnv := maps.Clone(before.Environment), maps.Clone(after.Environment)
	if before.Spec.Kind == "profile" {
		delete(leftEnv, "UNOBIN_BENCH_PROFILE")
		delete(rightEnv, "UNOBIN_BENCH_PROFILE")
		if len(before.Capabilities) == 0 || len(after.Capabilities) == 0 {
			return Summary{}, fmt.Errorf("profile comparisons require both capability sets")
		}
	} else if !slices.Equal(before.Capabilities, after.Capabilities) {
		return Summary{}, fmt.Errorf("capabilities differ in a performance comparison")
	}
	if !maps.Equal(leftEnv, rightEnv) {
		return Summary{}, fmt.Errorf("target, compiler flags, or cache settings differ")
	}
	keys := slices.SortedFunc(maps.Keys(left), func(a, b Key) int {
		return cmp.Or(cmp.Compare(a.Package, b.Package), cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.CPU, b.CPU), cmp.Compare(a.Unit, b.Unit))
	})
	summary := Summary{
		FormatVersion: FormatVersion, WorkPackage: before.Spec.WorkPackage,
		BeforeRevision: before.Revision, AfterRevision: after.Revision,
		FixtureRevision: before.FixtureRevision, PrimaryUnit: before.Spec.PrimaryUnit,
		Statistics: []Statistic{}, Warnings: []string{},
	}
	if before.Spec.Kind == "profile" {
		summary.Warnings = append(summary.Warnings,
			"Capability sets differ; assess the profile tradeoff separately from default performance.")
	}
	improved, regressed := false, false
	for _, key := range keys {
		first := benchmath.NewSample(slices.Clone(left[key]), &benchmath.DefaultThresholds)
		second := benchmath.NewSample(slices.Clone(right[key]), &benchmath.DefaultThresholds)
		a, b := benchmath.AssumeNothing.Summary(first, 0.95),
			benchmath.AssumeNothing.Summary(second, 0.95)
		comparison := benchmath.AssumeNothing.Compare(first, second)
		statistic := Statistic{
			Package: key.Package, Name: key.Name, CPU: key.CPU, Unit: key.Unit,
			Before: distribution(a, len(first.Values)), After: distribution(b, len(second.Values)),
			P: comparison.P, Alpha: comparison.Alpha, Significant: comparison.P < comparison.Alpha,
			Warnings: []string{},
		}
		if a.Center != 0 {
			delta := 100 * (b.Center/a.Center - 1)
			statistic.DeltaPercent = &delta
		} else if b.Center != 0 {
			statistic.Warnings = append(statistic.Warnings, "Relative change has a zero baseline.")
		}
		for _, warnings := range [][]error{a.Warnings, b.Warnings, comparison.Warnings} {
			for _, warning := range warnings {
				message := warning.Error()
				if !slices.Contains(statistic.Warnings, message) {
					statistic.Warnings = append(statistic.Warnings, message)
				}
			}
		}
		direction := "lower"
		if key.Unit == summary.PrimaryUnit {
			direction = before.Spec.PrimaryDirection
		}
		switch {
		case a.Center == b.Center:
			statistic.Assessment = "unchanged"
		case !statistic.Significant:
			statistic.Assessment = "inconclusive"
		case b.Center < a.Center && direction == "lower" ||
			b.Center > a.Center && direction == "higher":
			statistic.Assessment = "improved"
		default:
			statistic.Assessment = "regressed"
		}
		if key.Unit == summary.PrimaryUnit {
			improved = improved || statistic.Assessment == "improved"
			regressed = regressed || statistic.Assessment == "regressed"
		}
		summary.Statistics = append(summary.Statistics, statistic)
	}
	switch {
	case improved && regressed:
		summary.Assessment = "mixed"
	case improved:
		summary.Assessment = "improved"
	case regressed:
		summary.Assessment = "regressed"
	default:
		summary.Assessment = "inconclusive"
	}
	return summary, nil
}

func distribution(summary benchmath.Summary, samples int) Distribution {
	result := Distribution{Samples: samples, Median: summary.Center, Confidence: summary.Confidence}
	if finite(summary.Lo) {
		result.Low = &summary.Lo
	}
	if finite(summary.Hi) {
		result.High = &summary.Hi
	}
	return result
}
