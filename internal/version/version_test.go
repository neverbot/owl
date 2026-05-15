package version

import "testing"

func TestStringDefaultsToDev(t *testing.T) {
	if got := String(); got != "dev" {
		t.Fatalf("String() = %q, want %q", got, "dev")
	}
}
