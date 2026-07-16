package contract

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifyRejectsTamperingAndExtraArtifacts(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	source := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "contracts", "studio", "v1"))

	for _, test := range []struct {
		name   string
		mutate func(string) error
	}{
		{name: "tampered artifact", mutate: func(directory string) error {
			return os.WriteFile(filepath.Join(directory, "README.md"), []byte("tampered\n"), 0o600)
		}},
		{name: "extra artifact", mutate: func(directory string) error {
			return os.WriteFile(filepath.Join(directory, "unexpected.json"), []byte("{}\n"), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			destination := t.TempDir()
			for _, name := range artifactNames {
				contents, err := os.ReadFile(filepath.Join(source, name))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(destination, name), contents, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := test.mutate(destination); err != nil {
				t.Fatal(err)
			}
			if _, err := Verify(destination); err == nil {
				t.Fatal("Verify accepted contract drift")
			}
		})
	}
}
