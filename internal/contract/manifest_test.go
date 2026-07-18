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
	if result.Version != "1.1.0" {
		t.Fatalf("contract version = %q, want 1.1.0", result.Version)
	}
	if result.SourceFingerprint != "3a5a4bb8e35cb66ff3374f7a28d5d401684fcc56441aca85df75d64c8c922f19" {
		t.Fatalf("source fingerprint = %q", result.SourceFingerprint)
	}
}
