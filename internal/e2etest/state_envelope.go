package e2etest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type stateEnvelopeSummary struct {
	EnvelopeVersion int               `json:"envelope-version"`
	PayloadType     state.PayloadType `json:"payload-type,omitempty"`
	Encrypter       *state.Ref        `json:"encrypter,omitempty"`
	CiphertextJSON  bool              `json:"ciphertext-json"`
}

func compareStateEnvelopes(
	caseDir string,
	workspace string,
	c CompiledCase,
	doUpdate bool,
) error {
	for _, check := range c.StateEnvelopes {
		body, err := stateEnvelopeSummaryJSON(workspace, c.Name, check.Stack)
		if err != nil {
			return err
		}
		if err := compareOptionalGolden(caseDir, check.Stack, check.Want, body, doUpdate); err != nil {
			return err
		}
	}
	return nil
}

func stateEnvelopeSummaryJSON(workspace string, factoryName string, stack string) (string, error) {
	root := filepath.Join(workspace, ".unobin", "state", factoryName, filepath.FromSlash(stack))
	revBody, err := os.ReadFile(filepath.Join(root, "current"))
	if err != nil {
		return "", fmt.Errorf("read current state rev: %w", err)
	}
	rev := strings.TrimSpace(string(revBody))
	body, err := os.ReadFile(filepath.Join(root, "snapshots", rev+".json.enc"))
	if err != nil {
		return "", fmt.Errorf("read current state envelope: %w", err)
	}
	var env state.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("decode current state envelope: %w", err)
	}
	summary := stateEnvelopeSummary{
		EnvelopeVersion: env.EnvelopeVersion,
		PayloadType:     env.PayloadType,
		Encrypter:       env.Encrypter,
		CiphertextJSON:  json.Valid(env.Ciphertext),
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(summary); err != nil {
		return "", err
	}
	return out.String(), nil
}
