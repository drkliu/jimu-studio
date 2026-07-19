package scorecard

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestEveryContractOperationHasAProductionScoreAndRecord(t *testing.T) {
	t.Parallel()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	if err := Verify(root); err != nil {
		t.Fatal(err)
	}
}
