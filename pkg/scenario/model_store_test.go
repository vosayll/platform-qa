package scenario

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validScenario() *Scenario {
	return &Scenario{
		Key:   "smoke_custom",
		Title: "Smoke Custom",
		Steps: []Step{
			{ID: "step_one", Title: "HTTP", Type: "http", Role: "none", Method: "POST", Path: "/api/clients/register",
				Body:    json.RawMessage(`{"phoneNumber":"+79990000000"}`),
				ExpectStatus: "4xx",
				Asserts: []Assert{{Path: "$.error", Op: "exists"}},
			},
			{ID: "wait_a_bit", Title: "Пауза", Type: "delay", MS: 100},
			{ID: "check_var", Title: "Проверка", Type: "assert", Left: "{{uuid}}", Check: &Assert{Op: "notEmpty"}},
		},
	}
}

func TestValidateOK(t *testing.T) {
	sc := validScenario()
	if err := sc.Validate(); err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
}

func TestValidateKeyFormat(t *testing.T) {
	sc := validScenario()
	sc.Key = "Bad-Key"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected key format error")
	}
	sc.Key = "ab" // too short
	if err := sc.Validate(); err == nil {
		t.Fatal("expected key length error")
	}
}

func TestValidateTitleAndStepsRequired(t *testing.T) {
	sc := validScenario()
	sc.Title = ""
	if err := sc.Validate(); err == nil {
		t.Fatal("expected missing title error")
	}
	sc = validScenario()
	sc.Steps = nil
	if err := sc.Validate(); err == nil {
		t.Fatal("expected empty steps error")
	}
	sc = validScenario()
	for i := 0; i < 51; i++ {
		sc.Steps = append(sc.Steps, Step{ID: stepID(i), Type: "delay", MS: 1})
	}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected >50 steps error")
	}
}

func stepID(i int) string {
	return "s_" + jsonNumber(i)
}

func jsonNumber(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestValidateDuplicateStepIDs(t *testing.T) {
	sc := validScenario()
	sc.Steps[1].ID = "step_one"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestValidateStepIDCase(t *testing.T) {
	sc := validScenario()
	sc.Steps[0].ID = "Step_One"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected non-snake_case id error")
	}
}

func TestValidateHTTPFields(t *testing.T) {
	sc := validScenario()
	sc.Steps[0].Method = "FETCH"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected invalid method error")
	}
	sc = validScenario()
	sc.Steps[0].Path = "api/no-slash"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected path prefix error")
	}
	sc = validScenario()
	sc.Steps[0].Role = "root"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected invalid role error")
	}
}

func TestValidateExpectStatus(t *testing.T) {
	sc := validScenario()
	sc.Steps[0].ExpectStatus = float64(99)
	if err := sc.Validate(); err == nil {
		t.Fatal("expected low status error")
	}
	sc = validScenario()
	sc.Steps[0].ExpectStatus = "9xx"
	if err := sc.Validate(); err == nil {
		t.Fatal("expected bad class error")
	}
	sc = validScenario()
	sc.Steps[0].ExpectStatus = float64(599)
	if err := sc.Validate(); err != nil {
		t.Fatalf("expected 599 to be valid, got %v", err)
	}
	sc = validScenario()
	sc.Steps[0].ExpectStatus = "2xx"
	if err := sc.Validate(); err != nil {
		t.Fatalf("expected 2xx class to be valid, got %v", err)
	}
}

func TestValidateAssertOps(t *testing.T) {
	sc := validScenario()
	sc.Steps[0].Asserts = []Assert{{Path: "$.a", Op: "notEmpty"}}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected http assert op error (notEmpty is check-only)")
	}
	sc = validScenario()
	sc.Steps[0].Asserts = []Assert{{Path: "a.b", Op: "eq"}}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected JSONPath prefix error")
	}
	sc = validScenario()
	sc.Steps[2].Check = &Assert{Op: "exists"}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected check op error (exists is http-assert-only)")
	}
}

func TestValidateDependsOnSelf(t *testing.T) {
	sc := validScenario()
	sc.DependsOn = []string{"flow_a", "smoke_custom"}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected self-dependency error")
	}
}

func TestStoreSaveListGetDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scenarios")

	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sc := validScenario()
	if err := st.Save(sc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !st.Exists("smoke_custom") {
		t.Fatal("Exists after Save = false")
	}

	// tmp file must not leak
	if _, err := os.Stat(filepath.Join(dir, "smoke_custom.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("tmp file leaked")
	}

	got, ok := st.Get("smoke_custom")
	if !ok || got.Title != "Smoke Custom" || len(got.Steps) != 3 || got.Category != "custom" {
		t.Fatalf("Get mismatch: %+v ok=%v", got, ok)
	}

	if lst := st.List(); len(lst) != 1 {
		t.Fatalf("List len = %d, want 1", len(lst))
	}

	if err := st.Delete("smoke_custom"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if st.Exists("smoke_custom") {
		t.Fatal("Exists after Delete = true")
	}
	if _, err := os.Stat(filepath.Join(dir, "smoke_custom.json")); !os.IsNotExist(err) {
		t.Fatal("file not removed from disk")
	}
	if err := st.Delete("smoke_custom"); err == nil {
		t.Fatal("expected not-found error on second delete")
	}
}

func TestStoreReloadFromDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scenarios")

	st1, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := st1.Save(validScenario()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	st2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	if !st2.Exists("smoke_custom") {
		t.Fatal("scenario not reloaded from disk")
	}
}

func TestStoreRejectsInvalidOnSave(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "scenarios"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sc := validScenario()
	sc.Key = "INVALID"
	if err := st.Save(sc); err == nil {
		t.Fatal("expected validation error on save")
	}
}
