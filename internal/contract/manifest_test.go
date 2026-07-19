package contract

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifyPinnedStudioContract(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	result, err := Verify(filepath.Join(root, "contracts", "studio", "v1"))
	if err != nil {
		t.Fatalf("verify pinned contract: %v", err)
	}
	if result.Version != "1.2.0" {
		t.Fatalf("contract version = %q, want 1.2.0", result.Version)
	}
	if result.SourceFingerprint != "f797f4650bb62753dc09adb9a713b2b45a54c1b70b58365c9466a404d45ac52e" {
		t.Fatalf("source fingerprint = %q", result.SourceFingerprint)
	}
}
