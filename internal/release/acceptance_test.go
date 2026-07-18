package release

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVerifyReleasedAcceptance(t *testing.T) {
	root := repositoryRoot(t)
	if err := Verify(root); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsAcceptedReleaseClaims(t *testing.T) {
	root := copyMetadata(t)
	path := filepath.Join(root, "release", "acceptance.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), `"status": "released"`, `"status": "accepted"`, 1))
	if err = os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = Verify(root); err == nil || !strings.Contains(err.Error(), "accepted record claims") {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsContractDigestDrift(t *testing.T) {
	root := copyMetadata(t)
	path := filepath.Join(root, "release", "acceptance.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "8edd303a", "00000000", 1))
	if err = os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = Verify(root); err == nil || !strings.Contains(err.Error(), "digests mismatch") {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsIncompleteReleasedSource(t *testing.T) {
	root := copyMetadata(t)
	path := filepath.Join(root, "release", "acceptance.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), `"source_commit": "2db2c8bcd877174c068f65ed034303c876da7834"`, `"source_commit": ""`, 1))
	if err = os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = Verify(root); err == nil || !strings.Contains(err.Error(), "incomplete source identity") {
		t.Fatalf("Verify() error = %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func copyMetadata(t *testing.T) string {
	t.Helper()
	source := repositoryRoot(t)
	root := t.TempDir()
	for _, name := range []string{
		filepath.Join("release", "acceptance.json"),
		filepath.Join("release", "provider-contract.json"),
		filepath.Join("contracts", "studio", "v1", "manifest.json"),
	} {
		contents, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, name)
		if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
