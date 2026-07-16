// Package config loads the Studio's explicit tenant and OIDC registrations.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

var safeEnvironmentName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

// File is the non-secret JSON configuration. OIDC secrets are environment references.
type File struct {
	Development bool     `json:"development"`
	Tenants     []Tenant `json:"tenants"`
}

// Tenant defines one explicit provider origin and standard OIDC registration.
type Tenant struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ProviderBaseURL string `json:"provider_base_url"`
	Issuer          string `json:"issuer"`
	ClientID        string `json:"client_id"`
	ClientSecretEnv string `json:"client_secret_env"`
	RedirectURL     string `json:"redirect_url"`
	RoleClaim       string `json:"role_claim,omitempty"`
}

// Load reads one bounded JSON object and validates all non-secret configuration.
func Load(path string) (File, error) {
	if strings.TrimSpace(path) == "" {
		return File{}, errors.New("STUDIO_CONFIG must name a configuration file")
	}
	input, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("open Studio configuration: %w", err)
	}
	defer input.Close()
	decoder := json.NewDecoder(&boundedReader{reader: input, remaining: 1 << 20})
	decoder.DisallowUnknownFields()
	var file File
	if err = decoder.Decode(&file); err != nil {
		return File{}, fmt.Errorf("decode Studio configuration: %w", err)
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return File{}, errors.New("Studio configuration must contain exactly one JSON object")
	}
	if err = file.Validate(); err != nil {
		return File{}, err
	}
	return file, nil
}

// Validate rejects ambiguous registrations and non-TLS production endpoints.
func (file File) Validate() error {
	if len(file.Tenants) == 0 || len(file.Tenants) > 128 {
		return errors.New("Studio configuration requires 1 to 128 tenants")
	}
	seen := make(map[string]struct{}, len(file.Tenants))
	for index, tenant := range file.Tenants {
		if tenant.ID == "" || tenant.Name == "" || tenant.ClientID == "" || !safeEnvironmentName.MatchString(tenant.ClientSecretEnv) {
			return fmt.Errorf("tenant %d has incomplete identity configuration", index)
		}
		if _, exists := seen[tenant.ID]; exists {
			return fmt.Errorf("duplicate tenant %q", tenant.ID)
		}
		seen[tenant.ID] = struct{}{}
		for label, raw := range map[string]string{"issuer": tenant.Issuer, "provider": tenant.ProviderBaseURL, "redirect": tenant.RedirectURL} {
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("tenant %q has invalid %s URL", tenant.ID, label)
			}
			if (!file.Development && parsed.Scheme != "https") || (file.Development && parsed.Scheme != "https" && parsed.Scheme != "http") {
				return fmt.Errorf("tenant %q %s URL requires HTTPS", tenant.ID, label)
			}
		}
	}
	return nil
}

// Build performs OIDC discovery and creates a volatile broker.
func (file File) Build(ctx context.Context, getenv func(string) string) (*auth.Broker, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	tenants := make([]auth.Tenant, 0, len(file.Tenants))
	for _, item := range file.Tenants {
		secret := getenv(item.ClientSecretEnv)
		if secret == "" {
			return nil, fmt.Errorf("tenant %q client secret environment variable is unavailable", item.ID)
		}
		discoveryContext, cancelDiscovery := context.WithTimeout(ctx, 15*time.Second)
		identity, err := auth.NewOIDCProvider(discoveryContext, auth.OIDCConfig{
			Issuer: item.Issuer, ClientID: item.ClientID, ClientSecret: secret,
			RedirectURL: item.RedirectURL, RoleClaim: item.RoleClaim,
		})
		cancelDiscovery()
		if err != nil {
			return nil, fmt.Errorf("initialize tenant %q OIDC: %w", item.ID, err)
		}
		tenants = append(tenants, auth.Tenant{ID: item.ID, Name: item.Name, ProviderBaseURL: item.ProviderBaseURL, Identity: identity})
	}
	return auth.NewBroker(auth.Config{
		Tenants: tenants,
		ClientFactory: func(ctx context.Context, baseURL, token string) (auth.TenantClient, error) {
			return provider.NewClient(ctx, baseURL, token, nil)
		},
	})
}

type boundedReader struct {
	reader    *os.File
	remaining int64
}

func (reader *boundedReader) Read(buffer []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, errors.New("Studio configuration exceeds 1 MiB")
	}
	if int64(len(buffer)) > reader.remaining {
		buffer = buffer[:reader.remaining]
	}
	read, err := reader.reader.Read(buffer)
	reader.remaining -= int64(read)
	return read, err
}
