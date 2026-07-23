package check

import (
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type assetReference struct {
	object    string
	attribute string
}

type assetReferenceReporter func(pos lang.Position, message, hint string)

func (c *referenceChecker) checkAsset(path *lang.DotPath, scope string) {
	resolveAssetReference(path, c.assetSets[scope], c.assetNames[scope], func(
		pos lang.Position,
		message string,
		hint string,
	) {
		c.addDiagnostic(pos, message, hint)
	})
}

func (c *referenceChecker) lookupAssetFor(scope string) typecheck.LookupAssetFn {
	return func(path *lang.DotPath) (typecheck.Type, bool) {
		reference, ok := resolveAssetReference(
			path,
			c.assetSets[scope],
			c.assetNames[scope],
			nil,
		)
		if !ok {
			return typecheck.TUnknown(), false
		}
		return assetReferenceType(reference.attribute), true
	}
}

func resolveAssetReference(
	path *lang.DotPath,
	set *asset.Set,
	declaredNames map[string]bool,
	report assetReferenceReporter,
) (assetReference, bool) {
	if path == nil || path.Root == nil {
		return assetReference{}, false
	}
	if len(path.Segments) == 0 {
		reportAssetReference(
			report,
			path.Root.S.Start,
			"asset requires a name",
			"write asset.<name>",
		)
		return assetReference{}, false
	}
	for _, segment := range path.Segments {
		if segment.Guarded {
			reportAssetReference(
				report,
				segment.S.Start,
				"asset references do not support guarded access",
				"",
			)
			return assetReference{}, false
		}
	}

	nameSegment := path.Segments[0]
	if nameSegment.Name == "" {
		hint := ""
		if literal, ok := nameSegment.Index.(*lang.StringLit); ok && literal.Value != "" {
			hint = "use asset." + literal.Value
		}
		reportAssetReference(
			report,
			nameSegment.S.Start,
			"asset names use dot access",
			hint,
		)
		return assetReference{}, false
	}
	if nameSegment.Index != nil || nameSegment.Splat {
		reportAssetReference(
			report,
			nameSegment.S.Start,
			"asset names use dot access",
			"",
		)
		return assetReference{}, false
	}

	name := nameSegment.Name
	var item *asset.Asset
	var entry *asset.Entry
	if set == nil && !declaredNames[name] {
		reportAssetReference(
			report,
			nameSegment.S.Start,
			fmt.Sprintf("unknown asset %q", name),
			"",
		)
		return assetReference{}, false
	}
	if set != nil {
		var ok bool
		item, ok = set.Asset(name)
		if !ok {
			reportAssetReference(
				report,
				nameSegment.S.Start,
				fmt.Sprintf("unknown asset %q", name),
				"",
			)
			return assetReference{}, false
		}
		entry, ok = item.Entry("")
		if !ok {
			return assetReference{}, false
		}
	}
	reference := assetReference{object: "asset." + name}
	rest := path.Segments[1:]
	if len(rest) == 0 {
		return reference, true
	}

	if rest[0].Splat {
		reportAssetReference(
			report,
			rest[0].S.Start,
			reference.object+" does not support splats",
			"",
		)
		return assetReference{}, false
	}
	if rest[0].Index != nil {
		if entry != nil && entry.Kind == asset.EntryKindFile {
			reportAssetReference(
				report,
				rest[0].S.Start,
				reference.object+" is a file and cannot select an internal entry",
				"",
			)
			return assetReference{}, false
		}
		literal, literalOK := rest[0].Index.(*lang.StringLit)
		if !literalOK {
			reportAssetReference(
				report,
				rest[0].S.Start,
				reference.object+" internal entry selection must be a string literal",
				"",
			)
			return assetReference{}, false
		}
		if !validAssetInternalPath(literal.Value) {
			reportAssetReference(
				report,
				rest[0].S.Start,
				fmt.Sprintf(
					"%s internal entry path %q is invalid",
					reference.object,
					literal.Value,
				),
				"",
			)
			return assetReference{}, false
		}
		entryFound := true
		if item != nil {
			_, entryFound = item.Entry(literal.Value)
		}
		if !entryFound {
			reportAssetReference(
				report,
				rest[0].S.Start,
				fmt.Sprintf(
					"%s has no internal entry %q",
					reference.object,
					literal.Value,
				),
				"",
			)
			return assetReference{}, false
		}
		reference.object += "['" + literal.Value + "']"
		rest = rest[1:]
		if len(rest) == 0 {
			return reference, true
		}
		if rest[0].Splat {
			reportAssetReference(
				report,
				rest[0].S.Start,
				reference.object+" does not support splats",
				"",
			)
			return assetReference{}, false
		}
		if rest[0].Index != nil {
			hint := combinedAssetSelectionHint(reference.object, literal.Value, rest[0])
			reportAssetReference(
				report,
				rest[0].S.Start,
				"asset."+name+" accepts only one internal entry selection",
				hint,
			)
			return assetReference{}, false
		}
	}

	attributeSegment := rest[0]
	if attributeSegment.Name == "" {
		return assetReference{}, false
	}
	if !assetAttribute(attributeSegment.Name) {
		hint := ""
		message := fmt.Sprintf(
			"%s has no attribute %q",
			reference.object,
			attributeSegment.Name,
		)
		if item != nil && reference.object == "asset."+name {
			if _, found := item.Entry(attributeSegment.Name); found {
				message = reference.object + "." + attributeSegment.Name +
					" names an internal entry"
				hint = "use " + reference.object + "['" + attributeSegment.Name + "']"
			}
		}
		reportAssetReference(report, attributeSegment.S.Start, message, hint)
		return assetReference{}, false
	}
	reference.attribute = attributeSegment.Name
	if len(rest) > 1 {
		reportAssetReference(
			report,
			rest[1].S.Start,
			reference.object+"."+reference.attribute+
				" cannot be followed by another segment",
			"",
		)
		return assetReference{}, false
	}
	return reference, true
}

func reportAssetReference(
	report assetReferenceReporter,
	pos lang.Position,
	message string,
	hint string,
) {
	if report != nil {
		report(pos, message, hint)
	}
}

func validAssetInternalPath(value string) bool {
	return utf8.ValidString(value) &&
		!strings.Contains(value, `\`) &&
		value != "." &&
		fs.ValidPath(value)
}

func combinedAssetSelectionHint(
	object string,
	first string,
	second lang.DotSegment,
) string {
	literal, ok := second.Index.(*lang.StringLit)
	if !ok || !validAssetInternalPath(literal.Value) {
		return ""
	}
	combined := first + "/" + literal.Value
	if !validAssetInternalPath(combined) {
		return ""
	}
	return "use " + strings.SplitN(object, "[", 2)[0] + "['" + combined + "']"
}

func assetAttribute(name string) bool {
	switch name {
	case "path", "content", "content-sha256", "mode":
		return true
	default:
		return false
	}
}

func assetReferenceType(attribute string) typecheck.Type {
	switch attribute {
	case "":
		return typecheck.TObject([]typecheck.ObjectField{
			{Name: "path", Type: typecheck.TAssetPath()},
			{Name: "content", Type: typecheck.TBytes()},
			{Name: "content-sha256", Type: typecheck.TString()},
			{Name: "mode", Type: typecheck.TString()},
		})
	case "path":
		return typecheck.TAssetPath()
	case "content":
		return typecheck.TBytes()
	case "content-sha256", "mode":
		return typecheck.TString()
	default:
		return typecheck.TUnknown()
	}
}
