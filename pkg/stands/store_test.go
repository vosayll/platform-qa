package stands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewStoreSeedsDefaultOnFreshDir(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, Stand{Name: "Встроенный мок", BaseURL: "http://127.0.0.1:65500", IsMock: true})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 stand, got %d", len(list))
	}
	if list[0].Name != "Встроенный мок" || !list[0].IsMock {
		t.Fatalf("unexpected stand: %+v", list[0])
	}
	if list[0].ID != "vstroennyy-mok" {
		t.Fatalf("unexpected slug id: %q", list[0].ID)
	}
	act, ok := s.Active()
	if !ok || !act.IsMock {
		t.Fatalf("mock must be active by default, got %+v ok=%v", act, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "stands.json")); err != nil {
		t.Fatalf("stands.json must be created: %v", err)
	}
}

func TestAddValidation(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	cases := []struct {
		name, baseURL string
		wantErr       bool
	}{
		{"", "http://x.local", true},
		{"Staging", "", true},
		{"Staging", "ftp://x.local", true},
		{"Staging", "not a url", true},
		{"Staging", "http://localhost:9999", false},
	}
	for _, tc := range cases {
		_, err := s.Add(tc.name, tc.baseURL, "1111")
		if (err != nil) != tc.wantErr {
			t.Errorf("Add(%q,%q) err=%v wantErr=%v", tc.name, tc.baseURL, err, tc.wantErr)
		}
	}
	long := make([]byte, 61)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := s.Add(string(long), "http://x.local", ""); err == nil {
		t.Error("60+ char name must fail")
	}
}

func TestAddSlugCollision(t *testing.T) {
	s, _ := NewStore(t.TempDir(), Stand{Name: "Встроенный мок", BaseURL: fallbackURL, IsMock: true})
	a1, err := s.Add("Встроенный мок", "http://a.local", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	a2, err := s.Add("Встроенный мок", "http://b.local", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if a1.ID != "vstroennyy-mok-2" {
		t.Fatalf("first collision id = %q", a1.ID)
	}
	if a2.ID != "vstroennyy-mok-3" {
		t.Fatalf("second collision id = %q", a2.ID)
	}
}

func TestUpdateGuards(t *testing.T) {
	s, _ := NewStore(t.TempDir(), Stand{Name: "Встроенный мок", BaseURL: fallbackURL, IsMock: true})
	st, _ := s.Add("Staging", "http://staging.local", "0000")

	if err := s.Update(st.ID, "Staging2", "http://other.local", ""); err != nil {
		t.Fatalf("Update regular stand: %v", err)
	}
	got, _ := s.Get(st.ID)
	if got.Name != "Staging2" || got.BaseURL != "http://other.local" || got.VerifyCode != "" {
		t.Fatalf("update not applied: %+v", got)
	}
	mockID := s.List()[0].ID
	if err := s.Update(mockID, "Встроенный мок", "http://evil.local", ""); err == nil {
		t.Fatal("mock baseURL change must fail")
	}
	if err := s.Update("nope", "X", "http://x.local", ""); err == nil {
		t.Fatal("unknown id update must fail")
	}
	if err := s.Update(st.ID, "", "http://other.local", ""); err == nil {
		t.Fatal("empty name must fail")
	}
}

func TestDeleteGuardsAndOrder(t *testing.T) {
	s, _ := NewStore(t.TempDir(), Stand{Name: "Встроенный мок", BaseURL: fallbackURL, IsMock: true})
	mockID := s.List()[0].ID

	if _, err := s.Add("A", "http://a.local", ""); err != nil {
		t.Fatalf("Add A: %v", err)
	}
	aID := s.List()[1].ID

	if _, err := s.Activate(aID); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	if err := s.Delete(aID); err == nil {
		t.Fatal("deleting active stand must fail")
	}
	if _, err := s.Activate(mockID); err != nil {
		t.Fatalf("Activate mock: %v", err)
	}
	if err := s.Delete(mockID); err == nil {
		t.Fatal("deleting mock stand must fail")
	}
	if err := s.Delete(aID); err != nil {
		t.Fatalf("deleting inactive regular stand must pass: %v", err)
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != mockID {
		t.Fatalf("unexpected list after delete: %+v", list)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	def := Stand{Name: "Встроенный мок", BaseURL: fallbackURL, IsMock: true}
	s, _ := NewStore(dir, def)
	st, _ := s.Add("Staging", "http://staging.local", "7777")
	if _, err := s.Activate(st.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "stands.json"))
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	var p persisted
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("persisted file is not valid json: %v", err)
	}
	if p.ActiveID != st.ID || len(p.Stands) != 2 {
		t.Fatalf("persisted content: %+v", p)
	}

	reloaded, err := NewStore(dir, def)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	act, ok := reloaded.Active()
	if !ok || act.ID != st.ID || act.VerifyCode != "7777" {
		t.Fatalf("active stand lost after reload: %+v ok=%v", act, ok)
	}
	views := reloaded.ListWithActive()
	if views[0].IsActive {
		t.Fatal("mock must not be marked active after reload")
	}
	if !views[1].IsActive {
		t.Fatal("staging must be marked active after reload")
	}
}

func TestListKeepsInsertionOrder(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	for _, n := range []string{"Zeta", "Alpha", "Mid"} {
		if _, err := s.Add(n, "http://"+n+".local", ""); err != nil {
			t.Fatalf("Add %s: %v", n, err)
		}
	}
	list := s.List()
	want := []string{"osnovnoy-stend", "zeta", "alpha", "mid"}
	for i, id := range want {
		if list[i].ID != id {
			t.Fatalf("order broken at %d: want %s got %s (%+v)", i, id, list[i].ID, list)
		}
	}
}
