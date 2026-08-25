package registry

import "testing"

func TestCatalogOrderAndKeys(t *testing.T) {
	want := []string{"flow_a", "flow_b", "cancellation", "idempotency", "security_rbac", "negative_sm"}
	keys := Keys()
	if len(keys) != len(want) {
		t.Fatalf("Keys() = %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("Keys()[%d] = %q, want %q", i, keys[i], k)
		}
	}
	all := All()
	if len(all) != len(want) {
		t.Fatalf("All() length = %d, want %d", len(all), len(want))
	}
}

func TestGet(t *testing.T) {
	s, ok := Get("flow_a")
	if !ok || s.Key != "flow_a" || s.Category != "flow" || len(s.Checks) != 7 {
		t.Fatalf("Get(flow_a) unexpected: %+v ok=%v", s, ok)
	}
	if _, ok := Get("all"); ok {
		t.Fatal("virtual key 'all' must not be in the catalog")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown key must not be found")
	}
}

func TestCheckIDsStableAndUnique(t *testing.T) {
	for _, s := range All() {
		if len(s.Checks) == 0 {
			t.Fatalf("suite %s has no checks", s.Key)
		}
		seen := map[string]bool{}
		for _, c := range s.Checks {
			if seen[c.ID] {
				t.Fatalf("duplicate check id %s in suite %s", c.ID, s.Key)
			}
			seen[c.ID] = true
			for _, r := range c.ID {
				if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
					t.Fatalf("check id %q is not snake_case", c.ID)
				}
			}
		}
	}
}
