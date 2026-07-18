// Package release verifies the committed Studio release-acceptance record.
package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

const (
	wantStudioVersion = "1.0.0"
	wantGoVersion     = "go1.26.5"
	wantTag           = "v1.0.0"
	wantProvider      = "github.com/drkliu/jimu"
	wantContract      = "1.1.0"
)

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

type acceptance struct {
	SchemaVersion    int              `json:"schema_version"`
	Status           string           `json:"status"`
	StudioVersion    string           `json:"studio_version"`
	GoVersion        string           `json:"go_version"`
	Packaging        string           `json:"packaging"`
	Provider         providerIdentity `json:"provider"`
	Contract         contractIdentity `json:"contract"`
	Baseline         baseline         `json:"baseline"`
	Release          releaseIdentity  `json:"release"`
	Evidence         []gateEvidence   `json:"evidence"`
	Supported        supportedMatrix  `json:"supported"`
	KnownLimitations []string         `json:"known_limitations"`
	Rollback         rollbackContract `json:"rollback"`
}

type providerIdentity struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	GoVersion  string `json:"go_version"`
}

type contractIdentity struct {
	Version           string            `json:"version"`
	SourceFingerprint string            `json:"source_fingerprint"`
	SHA256            map[string]string `json:"sha256"`
}

type baseline struct {
	S5MergeCommit        string `json:"s5_merge_commit"`
	S5CIRun              int64  `json:"s5_ci_run"`
	S5CodeQLRun          int64  `json:"s5_codeql_run"`
	ReconciliationCommit string `json:"reconciliation_commit"`
	ReconciliationCIRun  int64  `json:"reconciliation_ci_run"`
	ReconciliationCodeQL int64  `json:"reconciliation_codeql_run"`
}

type releaseIdentity struct {
	Tag          string `json:"tag"`
	SourceCommit string `json:"source_commit"`
	URL          string `json:"url"`
	PublishedAt  string `json:"published_at"`
}

type gateEvidence struct {
	Name       string `json:"name"`
	RunID      int64  `json:"run_id"`
	Conclusion string `json:"conclusion"`
}

type supportedMatrix struct {
	Deployment string   `json:"deployment"`
	Browsers   []string `json:"browsers"`
	GoOnly     bool     `json:"go_only"`
}

type rollbackContract struct {
	FallbackCommit            string `json:"fallback_commit"`
	RequiresReauthentication  bool   `json:"requires_reauthentication"`
	ReversesProviderMutations bool   `json:"reverses_provider_mutations"`
}

type providerRecord struct {
	Repository        string            `json:"provider_repository"`
	Commit            string            `json:"provider_commit"`
	GoVersion         string            `json:"provider_go_version"`
	ReleaseStatus     string            `json:"provider_release_status"`
	ContractVersion   string            `json:"studio_contract_version"`
	SourceFingerprint string            `json:"openapi_source_fingerprint"`
	SHA256            map[string]string `json:"manifest_sha256"`
}

type contractManifest struct {
	FormatVersion   int               `json:"format_version"`
	ContractVersion string            `json:"contract_version"`
	SHA256          map[string]string `json:"sha256"`
}

// Verify checks release/acceptance.json against provider metadata and the frozen contract.
func Verify(root string) error {
	var record acceptance
	if err := decodeStrict(filepath.Join(root, "release", "acceptance.json"), &record); err != nil {
		return err
	}
	var provider providerRecord
	if err := decodeStrict(filepath.Join(root, "release", "provider-contract.json"), &provider); err != nil {
		return err
	}
	var manifest contractManifest
	if err := decodeStrict(filepath.Join(root, "contracts", "studio", "v1", "manifest.json"), &manifest); err != nil {
		return err
	}
	if err := verifyStatic(record, provider, manifest); err != nil {
		return err
	}
	return verifyState(record)
}

func verifyStatic(record acceptance, provider providerRecord, manifest contractManifest) error {
	if record.SchemaVersion != 1 || record.StudioVersion != wantStudioVersion || record.GoVersion != wantGoVersion {
		return fmt.Errorf("release identity schema=%d studio=%q go=%q", record.SchemaVersion, record.StudioVersion, record.GoVersion)
	}
	if record.Packaging != "source-commit; no binary reproducibility claim" {
		return fmt.Errorf("unsupported packaging claim %q", record.Packaging)
	}
	if record.Provider.Repository != wantProvider || record.Provider.Repository != provider.Repository || record.Provider.Commit != provider.Commit || record.Provider.GoVersion != provider.GoVersion {
		return fmt.Errorf("provider identity does not match provider-contract.json")
	}
	if provider.ReleaseStatus == "" {
		return fmt.Errorf("provider release status is empty")
	}
	if record.Contract.Version != wantContract || manifest.FormatVersion != 1 || record.Contract.Version != manifest.ContractVersion || record.Contract.Version != provider.ContractVersion {
		return fmt.Errorf("contract version does not match frozen manifest")
	}
	if record.Contract.SourceFingerprint == "" || record.Contract.SourceFingerprint != provider.SourceFingerprint {
		return fmt.Errorf("contract source fingerprint mismatch")
	}
	if !equalMap(record.Contract.SHA256, manifest.SHA256) || !equalMap(record.Contract.SHA256, provider.SHA256) {
		return fmt.Errorf("contract artifact digests mismatch")
	}
	if record.Release.Tag != wantTag {
		return fmt.Errorf("release tag = %q", record.Release.Tag)
	}
	if !fullCommit.MatchString(record.Baseline.S5MergeCommit) || !fullCommit.MatchString(record.Baseline.ReconciliationCommit) || record.Baseline.S5CIRun <= 0 || record.Baseline.S5CodeQLRun <= 0 || record.Baseline.ReconciliationCIRun <= 0 || record.Baseline.ReconciliationCodeQL <= 0 {
		return fmt.Errorf("baseline evidence is incomplete")
	}
	requiredGates := []string{"contract", "format", "vet", "unit-race-build", "browser-e2e", "dependency-review", "vulnerability", "codeql"}
	if len(record.Evidence) != len(requiredGates) {
		return fmt.Errorf("release evidence gate count = %d", len(record.Evidence))
	}
	seen := make(map[string]bool, len(record.Evidence))
	for _, gate := range record.Evidence {
		if !slices.Contains(requiredGates, gate.Name) || seen[gate.Name] {
			return fmt.Errorf("invalid or duplicate release gate %q", gate.Name)
		}
		seen[gate.Name] = true
	}
	if record.Supported.Deployment == "" || len(record.Supported.Browsers) != 1 || record.Supported.Browsers[0] != "Chrome on the protected Ubuntu runner" || !record.Supported.GoOnly {
		return fmt.Errorf("unsupported deployment/browser matrix")
	}
	if len(record.KnownLimitations) != 9 {
		return fmt.Errorf("known limitation count = %d", len(record.KnownLimitations))
	}
	for i, limitation := range record.KnownLimitations {
		if limitation == "" {
			return fmt.Errorf("known limitation %d is empty", i)
		}
	}
	if record.Rollback.FallbackCommit != record.Baseline.S5MergeCommit || !record.Rollback.RequiresReauthentication || record.Rollback.ReversesProviderMutations {
		return fmt.Errorf("rollback contract is unsafe")
	}
	return nil
}

func verifyState(record acceptance) error {
	switch record.Status {
	case "candidate":
		if record.Release.SourceCommit != "" || record.Release.URL != "" || record.Release.PublishedAt != "" {
			return fmt.Errorf("candidate claims remote release facts")
		}
		for _, gate := range record.Evidence {
			if gate.RunID != 0 || gate.Conclusion != "pending" {
				return fmt.Errorf("candidate gate %q claims completed evidence", gate.Name)
			}
		}
	case "released":
		if !fullCommit.MatchString(record.Release.SourceCommit) || record.Release.URL == "" {
			return fmt.Errorf("released record has incomplete source identity")
		}
		if _, err := time.Parse(time.RFC3339, record.Release.PublishedAt); err != nil {
			return fmt.Errorf("released publication time: %w", err)
		}
		for _, gate := range record.Evidence {
			if gate.RunID <= 0 || gate.Conclusion != "success" {
				return fmt.Errorf("released gate %q is incomplete", gate.Name)
			}
		}
	default:
		return fmt.Errorf("unsupported release status %q", record.Status)
	}
	return nil
}

func decodeStrict(path string, target any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(contents) > 64<<10 {
		return fmt.Errorf("release metadata %s is too large", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode %s: trailing JSON value", path)
		}
		return fmt.Errorf("decode %s trailing data: %w", path, err)
	}
	return nil
}

func equalMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
