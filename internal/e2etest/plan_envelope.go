package e2etest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planEnvelopeSummary struct {
	EnvelopeVersion int               `json:"envelope-version"`
	PayloadType     state.PayloadType `json:"payload-type,omitempty"`
	Encrypter       *state.Ref        `json:"encrypter,omitempty"`
	CiphertextJSON  bool              `json:"ciphertext-json"`
}

func comparePlanEnvelopes(
	caseDir string,
	workspace string,
	checks []PlanEnvelopeCheck,
	doUpdate bool,
) error {
	for _, check := range checks {
		body, err := planEnvelopeSummaryJSON(workspace, check.Path)
		if err != nil {
			return err
		}
		if err := compareOptionalGolden(caseDir, check.Path, check.Want, body, doUpdate); err != nil {
			return err
		}
	}
	return nil
}

func tamperPlanFiles(workspace string, paths []string) error {
	for _, relPath := range paths {
		path := filepath.Join(workspace, filepath.FromSlash(relPath))
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read plan envelope %s: %w", relPath, err)
		}
		var env state.Envelope
		if err := json.Unmarshal(body, &env); err != nil {
			return fmt.Errorf("decode plan envelope %s: %w", relPath, err)
		}
		if len(env.Ciphertext) == 0 {
			return fmt.Errorf("plan envelope %s has empty ciphertext", relPath)
		}
		env.Ciphertext[len(env.Ciphertext)-1] ^= 0xff
		out, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}
		out = append(out, '\n')
		if err := os.WriteFile(path, out, 0o600); err != nil {
			return fmt.Errorf("write plan envelope %s: %w", relPath, err)
		}
	}
	return nil
}

func planEnvelopeSummaryJSON(workspace string, relPath string) (string, error) {
	path := filepath.Join(workspace, filepath.FromSlash(relPath))
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read plan envelope %s: %w", relPath, err)
	}
	var env state.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("decode plan envelope %s: %w", relPath, err)
	}
	summary := planEnvelopeSummary{
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
