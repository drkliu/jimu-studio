package config

import "testing"

func TestDevelopmentModeIsConfinedToLoopback(t *testing.T) {
	t.Parallel()
	file := File{Development: true}
	if err := file.ValidateListenAddress("127.0.0.1:8080"); err != nil {
		t.Fatalf("loopback rejected: %v", err)
	}
	for _, address := range []string{"0.0.0.0:8080", ":8080", "studio.example:8080"} {
		if err := file.ValidateListenAddress(address); err == nil {
			t.Errorf("unsafe address %q accepted", address)
		}
	}
}
