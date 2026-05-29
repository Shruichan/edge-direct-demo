package httpapi

import "testing"

func TestBootstrapTokenIsDeterministic(t *testing.T) {
	a := BootstrapToken("secret", "store-1", "AP-001")
	b := BootstrapToken("secret", "store-1", "AP-001")
	if a != b {
		t.Fatal("token should be deterministic")
	}
}

func TestBootstrapTokenChangesWithInputs(t *testing.T) {
	base := BootstrapToken("secret", "store-1", "AP-001")
	cases := map[string]string{
		"different secret": BootstrapToken("secret2", "store-1", "AP-001"),
		"different tenant": BootstrapToken("secret", "store-2", "AP-001"),
		"different serial": BootstrapToken("secret", "store-1", "AP-002"),
	}
	for name, other := range cases {
		if base == other {
			t.Errorf("token collision: %s", name)
		}
	}
}

// Guards against an obvious concat ambiguity: if tenant and serial were just
// joined, "ab"+"c" would equal "a"+"bc". The separator byte should prevent that.
func TestBootstrapTokenSeparatorMatters(t *testing.T) {
	a := BootstrapToken("secret", "ab", "c")
	b := BootstrapToken("secret", "a", "bc")
	if a == b {
		t.Fatal("tenant/serial boundary ambiguous")
	}
}

func TestValidBootstrap(t *testing.T) {
	token := BootstrapToken("secret", "t1", "s1")
	if !ValidBootstrap("secret", "t1", "s1", token) {
		t.Fatal("should accept valid token")
	}
	if ValidBootstrap("secret", "t1", "s2", token) {
		t.Fatal("should reject wrong serial")
	}
	if ValidBootstrap("other", "t1", "s1", token) {
		t.Fatal("should reject wrong secret")
	}
}
