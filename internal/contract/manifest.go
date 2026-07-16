// Package contract verifies the immutable Studio provider artifacts before the
// application is built or tested. The provider CLI remains the authoritative
// conformance check; this package provides a fast, offline integrity gate.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const pinnedVersion = "1.1.0"

var artifactNames = []string{"README.md", "client.ts", "fixtures.json", "manifest.json", "openapi.json"}

type manifest struct {
	FormatVersion   int               `json:"format_version"`
	ContractVersion string            `json:"contract_version"`
	SHA256          map[string]string `json:"sha256"`
}

type openAPI struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	SourceFingerprint string `json:"x-jimu-source-fingerprint"`
}

// Result describes the verified, pinned contract identity.
type Result struct {
	Version           string
	SourceFingerprint string
}

// Verify rejects missing, extra, symlinked, stale, or tampered artifacts.
func Verify(directory string) (Result, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Result{}, fmt.Errorf("read contract directory: %w", err)
	}
	actualNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || entry.IsDir() {
			return Result{}, fmt.Errorf("contract artifact %q is not a regular file", entry.Name())
		}
		actualNames = append(actualNames, entry.Name())
	}
	sort.Strings(actualNames)
	if fmt.Sprint(actualNames) != fmt.Sprint(artifactNames) {
		return Result{}, fmt.Errorf("contract artifact set = %v, want %v", actualNames, artifactNames)
	}

	manifestBytes, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read contract manifest: %w", err)
	}
	var pinned manifest
	if err = json.Unmarshal(manifestBytes, &pinned); err != nil {
		return Result{}, fmt.Errorf("decode contract manifest: %w", err)
	}
	if pinned.FormatVersion != 1 || pinned.ContractVersion != pinnedVersion {
		return Result{}, fmt.Errorf("unsupported contract manifest format=%d version=%q", pinned.FormatVersion, pinned.ContractVersion)
	}
	if len(pinned.SHA256) != len(artifactNames)-1 {
		return Result{}, fmt.Errorf("contract manifest digest count = %d", len(pinned.SHA256))
	}
	for name, expected := range pinned.SHA256 {
		if name == "manifest.json" || !contains(artifactNames, name) {
			return Result{}, fmt.Errorf("contract manifest contains unexpected digest name %q", name)
		}
		contents, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil {
			return Result{}, fmt.Errorf("read contract artifact %q: %w", name, readErr)
		}
		digest := sha256.Sum256(contents)
		if actual := hex.EncodeToString(digest[:]); actual != expected {
			return Result{}, fmt.Errorf("contract artifact %q digest = %s, want %s", name, actual, expected)
		}
	}

	openAPIBytes, err := os.ReadFile(filepath.Join(directory, "openapi.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read OpenAPI artifact: %w", err)
	}
	var specification openAPI
	if err = json.Unmarshal(openAPIBytes, &specification); err != nil {
		return Result{}, fmt.Errorf("decode OpenAPI artifact: %w", err)
	}
	if specification.Info.Version != pinnedVersion || specification.SourceFingerprint == "" {
		return Result{}, fmt.Errorf("OpenAPI identity version=%q fingerprint=%q", specification.Info.Version, specification.SourceFingerprint)
	}
	return Result{Version: pinnedVersion, SourceFingerprint: specification.SourceFingerprint}, nil
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
