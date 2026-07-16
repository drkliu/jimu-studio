package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionRequiresHTTPSAndSecretReference(t *testing.T) {
	t.Parallel()
	file := File{Tenants: []Tenant{{
		ID: "acme", Name: "Acme", ProviderBaseURL: "http://provider.example",
		Issuer: "https://id.example", ClientID: "studio", ClientSecretEnv: "OIDC_SECRET",
		RedirectURL: "https://studio.example/auth/callback",
	}}}
	if err := file.Validate(); err == nil || !strings.Contains(err.Error(), "requires HTTPS") {
		t.Fatalf("Validate() error = %v, want HTTPS rejection", err)
	}
	file.Development = true
	file.Tenants[0].ClientSecretEnv = "literal-secret"
	if err := file.Validate(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("Validate() error = %v, want environment reference rejection", err)
	}
}

func TestLoadRejectsUnknownFieldsAndTrailingObjects(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"unknown":  `{"development":true,"unknown":true,"tenants":[]}`,
		"trailing": `{"development":true,"tenants":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "studio.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load() succeeded for invalid configuration")
			}
		})
	}
}
