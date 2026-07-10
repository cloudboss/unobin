package cmdout

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

type encodingDocument struct {
	Kind          string         `json:"kind"                    ub:"kind"`
	FormatVersion int            `json:"format-version"          ub:"format-version"`
	Text          string         `json:"text"                    ub:"text"`
	Unicode       string         `json:"unicode"                 ub:"unicode"`
	InvalidUTF8   string         `json:"invalid-utf8"            ub:"invalid-utf8"`
	Numbers       []any          `json:"numbers"                 ub:"numbers"`
	Values        map[string]any `json:"values"                  ub:"values"`
	Bytes         []byte         `json:"bytes"                   ub:"bytes"`
	When          time.Time      `json:"when"                    ub:"when"`
	Optional      *string        `json:"optional,omitempty"      ub:"optional,omitempty"`
	EmptyList     []string       `json:"empty-list"              ub:"empty-list"`
	EmptyMap      map[string]any `json:"empty-map"               ub:"empty-map"`
}

type encodingGolden struct {
	Documents     []encodedGolden      `json:"documents"`
	InvalidValues []invalidValueGolden `json:"invalid-values"`
	Writes        []writeGolden        `json:"writes"`
	RecordMatches bool                 `json:"record-matches-document"`
	Deterministic bool                 `json:"deterministic"`
}

type encodedGolden struct {
	Format Format `json:"format"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

type invalidValueGolden struct {
	Name        string `json:"name"`
	JSONError   string `json:"json-error"`
	UnobinError string `json:"unobin-error"`
}

type writeGolden struct {
	Name   string `json:"name"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

func TestEncodingGolden(t *testing.T) {
	document := encodingDocument{
		Kind:          "encoding-test",
		FormatVersion: 1,
		Text:          "<tag>&value>",
		Unicode:       "snowman ☃",
		InvalidUTF8:   string([]byte{'a', 0xff, 'b'}),
		Numbers:       []any{int64(-2), uint64(3), 1.25},
		Values: map[string]any{
			"z": true,
			"a": nil,
		},
		Bytes:     []byte{0, 1, 2},
		When:      time.Date(2026, 7, 9, 14, 32, 18, 123456789, time.FixedZone("offset", 3600)),
		EmptyList: []string{},
		EmptyMap:  map[string]any{},
	}
	result := encodingGolden{}
	for _, format := range []Format{FormatJSON, FormatUnobin, FormatText, "unknown"} {
		output, err := EncodeDocument(format, document)
		result.Documents = append(result.Documents, encodedGolden{
			Format: format,
			Output: string(output),
			Error:  cmdoutErrorString(err),
		})
	}

	invalidValues := machineInvalidValues()
	for _, invalid := range invalidValues {
		_, jsonErr := EncodeDocument(FormatJSON, invalid.value)
		_, unobinErr := EncodeDocument(FormatUnobin, invalid.value)
		result.InvalidValues = append(result.InvalidValues, invalidValueGolden{
			Name:        invalid.name,
			JSONError:   cmdoutErrorString(jsonErr),
			UnobinError: cmdoutErrorString(unobinErr),
		})
	}

	var success bytes.Buffer
	err := WriteDocument(&success, FormatJSON, document)
	result.Writes = append(result.Writes, writeGolden{
		Name: "success", Output: success.String(), Error: cmdoutErrorString(err),
	})

	short := &shortWriter{}
	err = WriteDocument(short, FormatJSON, document)
	result.Writes = append(result.Writes, writeGolden{
		Name: "short write", Output: short.String(), Error: cmdoutErrorString(err),
	})

	failed := &failingWriter{}
	err = WriteDocument(failed, FormatJSON, document)
	result.Writes = append(result.Writes, writeGolden{
		Name: "writer error", Output: failed.String(), Error: cmdoutErrorString(err),
	})

	notWritten := &bytes.Buffer{}
	err = WriteDocument(notWritten, FormatJSON, math.NaN())
	result.Writes = append(result.Writes, writeGolden{
		Name: "encoding before write", Output: notWritten.String(), Error: cmdoutErrorString(err),
	})

	var recordOutput bytes.Buffer
	err = WriteRecord(&recordOutput, FormatUnobin, document)
	result.Writes = append(result.Writes, writeGolden{
		Name: "record success", Output: recordOutput.String(), Error: cmdoutErrorString(err),
	})

	recordFailure := &failingWriter{}
	err = WriteRecord(recordFailure, FormatUnobin, document)
	result.Writes = append(result.Writes, writeGolden{
		Name: "record writer error", Output: recordFailure.String(), Error: cmdoutErrorString(err),
	})

	record, recordErr := EncodeRecord(FormatJSON, document)
	documentBytes, documentErr := EncodeDocument(FormatJSON, document)
	result.RecordMatches = recordErr == nil && documentErr == nil && bytes.Equal(record, documentBytes)

	want, err := EncodeDocument(FormatJSON, document)
	result.Deterministic = err == nil
	for range 5 {
		got, encodeErr := EncodeDocument(FormatJSON, document)
		if encodeErr != nil || !bytes.Equal(want, got) {
			result.Deterministic = false
		}
	}

	requireCmdoutGolden(t, "testdata/encode.json", result)
}

type invalidValue struct {
	name  string
	value any
}

func machineInvalidValues() []invalidValue {
	mapCycle := map[string]any{}
	mapCycle["self"] = mapCycle
	sliceCycle := make([]any, 1)
	sliceCycle[0] = sliceCycle
	pointerCycle := &cycleValue{}
	pointerCycle.Next = pointerCycle
	return []invalidValue{
		{name: "NaN", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
		{name: "non-string map key", value: map[int]string{1: "value"}},
		{
			name: "invalid UTF-8 map key collision",
			value: map[string]any{
				string([]byte{0xff}): 1,
				string([]byte{0xfe}): 2,
			},
		},
		{name: "function", value: func() {}},
		{name: "channel", value: make(chan int)},
		{name: "complex", value: complex(1, 2)},
		{name: "json number", value: json.Number("1")},
		{name: "duration", value: time.Second},
		{name: "JSON marshaler", value: jsonMarshaler{}},
		{name: "text marshaler", value: textMarshaler{}},
		{name: "Unobin marshaler", value: unobinMarshaler{}},
		{name: "map cycle", value: mapCycle},
		{name: "slice cycle", value: sliceCycle},
		{name: "pointer cycle", value: pointerCycle},
	}
}

type cycleValue struct {
	Next *cycleValue `json:"next" ub:"next"`
}

type jsonMarshaler struct{}

func (jsonMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"json"`), nil
}

type textMarshaler struct{}

func (textMarshaler) MarshalText() ([]byte, error) {
	return []byte("text"), nil
}

type unobinMarshaler struct{}

func (unobinMarshaler) MarshalUB() ([]byte, error) {
	return []byte("'unobin'"), nil
}

type shortWriter struct {
	bytes.Buffer
}

func (w *shortWriter) Write(value []byte) (int, error) {
	if len(value) == 0 {
		return 0, nil
	}
	return w.Buffer.Write(value[:len(value)-1])
}

type failingWriter struct {
	bytes.Buffer
}

func (w *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer failed")
}
