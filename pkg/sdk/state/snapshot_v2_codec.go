package state

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cloudboss/unobin/internal/strictjson"
)

func encodeSnapshotV2(snapshot SnapshotV2) ([]byte, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeSnapshotV2(data []byte) (SnapshotV2, error) {
	if err := strictjson.Validate(data); err != nil {
		return SnapshotV2{}, fmt.Errorf("snapshot: %w", err)
	}
	var header struct {
		FormatVersion int `json:"format-version"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&header); err == nil &&
		header.FormatVersion == 1 {
		return SnapshotV2{}, fmt.Errorf(
			"snapshot: obsolete alpha format; create a new plan or state",
		)
	}

	var snapshot SnapshotV2
	if err := strictjson.Decode(data, &snapshot); err != nil {
		return SnapshotV2{}, fmt.Errorf("snapshot: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return SnapshotV2{}, fmt.Errorf("snapshot: %w", err)
	}
	return snapshot, nil
}
