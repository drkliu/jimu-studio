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
	if result.Version != "1.0.0" {
		t.Fatalf("contract version = %q, want 1.0.0", result.Version)
	}
	if result.SourceFingerprint != "e41ab114195abcf5791ae1f3d4eb402d1c1877d69d4c863e64880f5b79f0bf91" {
		t.Fatalf("source fingerprint = %q", result.SourceFingerprint)
	}
}
