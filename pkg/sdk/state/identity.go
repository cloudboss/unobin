package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// IdentityRecord describes the identity definition and remote incarnation
// validated for one resource target.
type IdentityRecord struct {
	DefinitionDigest string  `json:"definition-digest"`
	Version          int     `json:"version"`
	StableID         *string `json:"stable-id,omitempty"`
}

// Validate checks the required identity metadata and optional stable ID.
func (r IdentityRecord) Validate() error {
	if !validIdentityDigest(r.DefinitionDigest) {
		return fmt.Errorf("definition digest must be a lowercase SHA-256 digest")
	}
	if r.Version < 1 {
		return fmt.Errorf("identity version must be greater than zero")
	}
	if r.StableID != nil && *r.StableID == "" {
		return fmt.Errorf("stable ID must not be empty")
	}
	return nil
}

func validIdentityDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
