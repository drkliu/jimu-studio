// Package scorecard verifies that every pinned Provider operation has an
// evidence-bound production score and all critical controls are satisfied.
package scorecard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Scores struct {
	ContractFidelity            int `json:"contract_fidelity"`
	AuthorizationTenantSecurity int `json:"authorization_tenant_security"`
	OperationSafety             int `json:"operation_safety"`
	AccessibleUX                int `json:"accessible_ux"`
	EvidenceOperability         int `json:"evidence_operability"`
}

type Controls struct {
	ContractMatch             bool `json:"contract_match"`
	RoleEnforced              bool `json:"role_enforced"`
	TenantServerDerived       bool `json:"tenant_server_derived"`
	MutationGuardedOrReadOnly bool `json:"mutation_guarded_or_read_only"`
	AuditRecordedOrReadOnly   bool `json:"audit_recorded_or_read_only"`
}

type scoredOperation struct {
	ID       string   `json:"id"`
	Method   string   `json:"method"`
	Path     string   `json:"path"`
	Roles    []string `json:"roles"`
	Scores   Scores   `json:"scores"`
	Total    int      `json:"total"`
	Evidence []string `json:"evidence"`
	Controls Controls `json:"critical_controls"`
}

type record struct {
	SchemaVersion       int               `json:"schema_version"`
	Status              string            `json:"status"`
	StudioVersion       string            `json:"studio_version"`
	ContractVersion     string            `json:"contract_version"`
	ProviderCommit      string            `json:"provider_commit"`
	ProductionThreshold int               `json:"production_threshold"`
	Weights             Scores            `json:"weights"`
	Operations          []scoredOperation `json:"operations"`
}

type openAPI struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]struct {
		ID    string   `json:"operationId"`
		Roles []string `json:"x-jimu-required-roles"`
	} `json:"paths"`
}

// Verify checks the scorecard against the pinned OpenAPI document and evidence files.
func Verify(root string) error {
	var scores record
	if err := decodeStrict(filepath.Join(root, "docs", "releases", "v1.1.0-operation-scorecard.json"), &scores); err != nil {
		return err
	}
	var contract openAPI
	if err := decodeJSON(filepath.Join(root, "contracts", "studio", "v1", "openapi.json"), &contract); err != nil {
		return err
	}
	if scores.SchemaVersion != 1 || (scores.Status != "candidate" && scores.Status != "accepted") || scores.StudioVersion != "1.1.0" || scores.ContractVersion != contract.Info.Version || !fullCommit.MatchString(scores.ProviderCommit) {
		return fmt.Errorf("scorecard identity is incomplete or mismatched")
	}
	if scores.ProductionThreshold != 90 || scoreTotal(scores.Weights) != 100 {
		return fmt.Errorf("scorecard production gate or weights drifted")
	}

	expected := make(map[string]scoredOperation)
	for path, methods := range contract.Paths {
		for method, operation := range methods {
			expected[operation.ID] = scoredOperation{ID: operation.ID, Method: strings.ToUpper(method), Path: path, Roles: operation.Roles}
		}
	}
	if len(expected) != len(scores.Operations) {
		return fmt.Errorf("scorecard operation count=%d contract=%d", len(scores.Operations), len(expected))
	}
	seen := make(map[string]bool, len(scores.Operations))
	for _, operation := range scores.Operations {
		want, exists := expected[operation.ID]
		if !exists || seen[operation.ID] {
			return fmt.Errorf("unexpected or duplicate scored operation %q", operation.ID)
		}
		seen[operation.ID] = true
		if operation.Method != want.Method || operation.Path != want.Path || !slices.Equal(operation.Roles, want.Roles) {
			return fmt.Errorf("operation %q method/path/roles mismatch", operation.ID)
		}
		if !withinWeights(operation.Scores, scores.Weights) || scoreTotal(operation.Scores) != operation.Total || operation.Total < scores.ProductionThreshold {
			return fmt.Errorf("operation %q score=%d is invalid", operation.ID, operation.Total)
		}
		if !allControls(operation.Controls) || len(operation.Evidence) < 2 {
			return fmt.Errorf("operation %q lacks critical controls or evidence", operation.ID)
		}
		for _, evidence := range operation.Evidence {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(evidence)))
			if err != nil || info.IsDir() {
				return fmt.Errorf("operation %q evidence %q is unavailable", operation.ID, evidence)
			}
		}
	}
	return nil
}

func decodeStrict(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func decodeJSON(path string, target any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err = json.Unmarshal(contents, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func scoreTotal(score Scores) int {
	return score.ContractFidelity + score.AuthorizationTenantSecurity + score.OperationSafety + score.AccessibleUX + score.EvidenceOperability
}

func withinWeights(score, weights Scores) bool {
	return score.ContractFidelity >= 0 && score.ContractFidelity <= weights.ContractFidelity &&
		score.AuthorizationTenantSecurity >= 0 && score.AuthorizationTenantSecurity <= weights.AuthorizationTenantSecurity &&
		score.OperationSafety >= 0 && score.OperationSafety <= weights.OperationSafety &&
		score.AccessibleUX >= 0 && score.AccessibleUX <= weights.AccessibleUX &&
		score.EvidenceOperability >= 0 && score.EvidenceOperability <= weights.EvidenceOperability
}

func allControls(controls Controls) bool {
	return controls.ContractMatch && controls.RoleEnforced && controls.TenantServerDerived &&
		controls.MutationGuardedOrReadOnly && controls.AuditRecordedOrReadOnly
}
