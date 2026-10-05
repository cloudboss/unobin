package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Runner func(context.Context, string, []string, string) (Run, error)

type Comparison struct {
	FormatVersion int    `json:"format-version"`
	Kind          string `json:"kind"`
	Before        Record `json:"before"`
	After         Record `json:"after"`
	Comparison    Run    `json:"comparison"`
	SummaryDigest string `json:"summary-sha256"`
}

func SaveRecord(dir string, record Record) error {
	return saveJSON(filepath.Join(dir, "metadata.json"), record)
}

func LoadRecord(dir string) (Record, map[string][]byte, error) {
	var record Record
	if err := readJSON(filepath.Join(dir, "metadata.json"), &record); err != nil {
		return Record{}, nil, err
	}
	files, err := recordFiles(dir, record)
	return record, files, err
}

func WriteComparison(
	ctx context.Context, beforeDir, afterDir, out string, runner Runner,
) (Summary, error) {
	before, left, err := LoadRecord(beforeDir)
	if err != nil {
		return Summary{}, fmt.Errorf("before record: %w", err)
	}
	after, right, err := LoadRecord(afterDir)
	if err != nil {
		return Summary{}, fmt.Errorf("after record: %w", err)
	}
	summary, err := Compare(before, left, after, right)
	if err != nil {
		return Summary{}, err
	}
	if runner == nil {
		return Summary{}, fmt.Errorf("comparison tool is required")
	}
	if err := createDirectory(out); err != nil {
		return Summary{}, err
	}
	before, err = copyRecordFiles(out, "before", before, left)
	if err != nil {
		return Summary{}, err
	}
	after, err = copyRecordFiles(out, "after", after, right)
	if err != nil {
		return Summary{}, err
	}
	summaryBytes, err := jsonBytes(summary)
	if err != nil {
		return Summary{}, err
	}
	if err := writeExclusive(filepath.Join(out, "summary.json"), summaryBytes); err != nil {
		return Summary{}, err
	}
	run, runErr := runner(ctx, out, comparisonArgs(), "comparison.txt")
	metadata := Comparison{
		FormatVersion: FormatVersion, Kind: "benchmark-comparison", Before: before, After: after,
		Comparison: run, SummaryDigest: contentDigest(summaryBytes),
	}
	if err := errors.Join(runErr,
		saveJSON(filepath.Join(out, "metadata.json"), metadata)); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func ValidateComparison(ctx context.Context, dir string, runner Runner) (Summary, error) {
	var metadata Comparison
	if err := readJSON(filepath.Join(dir, "metadata.json"), &metadata); err != nil {
		return Summary{}, err
	}
	if metadata.FormatVersion != FormatVersion || metadata.Kind != "benchmark-comparison" {
		return Summary{}, fmt.Errorf("unsupported comparison metadata")
	}
	left, err := recordFiles(dir, metadata.Before)
	if err != nil {
		return Summary{}, err
	}
	right, err := recordFiles(dir, metadata.After)
	if err != nil {
		return Summary{}, err
	}
	summary, err := Compare(metadata.Before, left, metadata.After, right)
	if err != nil {
		return Summary{}, err
	}
	computed, err := jsonBytes(summary)
	if err != nil {
		return Summary{}, err
	}
	stored, err := readRegular(filepath.Join(dir, "summary.json"))
	if err != nil {
		return Summary{}, err
	}
	if metadata.SummaryDigest != contentDigest(stored) || !bytes.Equal(computed, stored) {
		return Summary{}, fmt.Errorf("summary statistics do not match the raw measurements")
	}
	comparison, err := readRegular(filepath.Join(dir, "comparison.txt"))
	if err != nil {
		return Summary{}, err
	}
	previous := metadata.Before.Benchmark.Finished
	if metadata.After.Benchmark.Finished.After(previous) {
		previous = metadata.After.Benchmark.Finished
	}
	if _, err := validateRun(metadata.Comparison, comparisonArgs(),
		map[string][]byte{"comparison.txt": comparison}, previous); err != nil {
		return Summary{}, fmt.Errorf("comparison tool: %w", err)
	}
	if runner == nil {
		return Summary{}, fmt.Errorf("comparison tool is required")
	}
	temp, err := os.MkdirTemp("", "unobin-benchstat-*")
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	inputs := map[string][]byte{"before.txt": left["before.txt"], "after.txt": right["after.txt"]}
	for name, body := range inputs {
		if err := writeExclusive(filepath.Join(temp, name), body); err != nil {
			return Summary{}, err
		}
	}
	run, err := runner(ctx, temp, comparisonArgs(), "comparison.txt")
	if err != nil {
		return Summary{}, err
	}
	if run.ExitCode != 0 {
		return Summary{}, fmt.Errorf("comparison tool returned exit status %d", run.ExitCode)
	}
	fresh, err := readRegular(filepath.Join(temp, "comparison.txt"))
	if err != nil {
		return Summary{}, err
	}
	if !bytes.Equal(fresh, comparison) {
		return Summary{}, fmt.Errorf("comparison output does not match the pinned tool")
	}
	return summary, nil
}

func comparisonArgs() []string {
	return []string{"go", "tool", "benchstat", "before.txt", "after.txt"}
}

func recordFiles(dir string, record Record) (map[string][]byte, error) {
	files := map[string][]byte{}
	runs := append(append([]Run{}, record.Tests...), record.Warmup, record.Benchmark)
	for _, run := range runs {
		if !safePath(run.Output) || filepath.Base(run.Output) != run.Output {
			return nil, fmt.Errorf("invalid recorded output path %q", run.Output)
		}
		body, err := readRegular(filepath.Join(dir, run.Output))
		if err != nil {
			return nil, err
		}
		files[run.Output] = body
	}
	return files, nil
}

func copyRecordFiles(dir, prefix string, record Record, files map[string][]byte) (Record, error) {
	record.Tests = append([]Run{}, record.Tests...)
	runs := []*Run{&record.Warmup, &record.Benchmark}
	for i := range record.Tests {
		runs = append(runs, &record.Tests[i])
	}
	for _, run := range runs {
		name := prefix + "-" + run.Output
		if run == &record.Benchmark {
			name = prefix + ".txt"
		}
		if err := writeExclusive(filepath.Join(dir, name), files[run.Output]); err != nil {
			return Record{}, err
		}
		run.Output = name
	}
	return record, nil
}

func createDirectory(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	return os.Mkdir(dir, 0o755)
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("evidence file %s is not a regular file", path)
	}
	return os.ReadFile(path)
}

func readJSON(path string, value any) error {
	body, err := readRegular(path)
	if err != nil {
		return err
	}
	if err := decodeJSON(body, value); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func decodeJSON(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

func jsonBytes(value any) ([]byte, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	return append(body, '\n'), err
}

func saveJSON(path string, value any) error {
	body, err := jsonBytes(value)
	if err != nil {
		return err
	}
	return writeExclusive(path, body)
}

func writeExclusive(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	return errors.Join(writeErr, file.Close())
}

func contentDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
