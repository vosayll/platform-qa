package report

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/runner"
)

func testRun() *runner.TestRun {
	return &runner.TestRun{
		ID:           "11111111-2222-3333-4444-555555555555",
		SuiteName:    "flow_a",
		SuiteKey:     "flow_a",
		Status:       "FAILED",
		StartTime:    time.UnixMilli(1700000000000),
		EndTime:      time.UnixMilli(1700000005000),
		DurationMs:   5000,
		Error:        `boom <tag> & "quote" 'apostrophe'`,
		TotalChecks:  7,
		PassedChecks: 1,
		FailedChecks: 1,
		Results: map[string]runner.CheckResult{
			"setup":        {Status: runner.CheckPassed, DurationMs: 1500},
			"create_order": {Status: runner.CheckFailed, Message: `broken <a> & "b"`, DurationMs: 250},
			"cooking":      {Status: runner.CheckSkipped, Message: "прогон завершился раньше"},
		},
	}
}

func TestJUnitXMLEscapingAndStructure(t *testing.T) {
	data := JUnitXML(testRun())

	var doc struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Time     string   `xml:"time,attr"`
		Suites   []struct {
			Name     string `xml:"name,attr"`
			Tests    int    `xml:"tests,attr"`
			Failures int    `xml:"failures,attr"`
			Skipped  int    `xml:"skipped,attr"`
			Cases    []struct {
				Name      string `xml:"name,attr"`
				Classname string `xml:"classname,attr"`
				Time      string `xml:"time,attr"`
				Failure   []struct {
					Message string `xml:"message,attr"`
					Body    string `xml:",chardata"`
				} `xml:"failure"`
				Skipped []struct {
					Message string `xml:"message,attr"`
				} `xml:"skipped"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, data)
	}
	if doc.Tests != 3 || doc.Failures != 1 {
		t.Fatalf("totals: tests=%d failures=%d, want 3/1", doc.Tests, doc.Failures)
	}
	if len(doc.Suites) != 1 {
		t.Fatalf("suites=%d, want 1", len(doc.Suites))
	}
	su := doc.Suites[0]
	if su.Name != "Flow A: Полный цикл ресторанного заказа" {
		t.Fatalf("suite name=%q", su.Name)
	}
	if su.Tests != 3 || su.Failures != 1 || su.Skipped != 1 {
		t.Fatalf("suite attrs: %+v", su)
	}

	raw := string(data)
	if !strings.Contains(raw, `<failure message="broken &lt;a&gt; &amp; &#34;b&#34;">broken &lt;a&gt; &amp; &#34;b&#34;</failure>`) {
		t.Fatalf("failure not escaped properly:\n%s", raw)
	}
	var byName = map[string]struct{ fail, skip bool }{}
	for _, c := range su.Cases {
		byName[c.Name] = struct{ fail, skip bool }{len(c.Failure) > 0, len(c.Skipped) > 0}
		if c.Classname != "flow_a" {
			t.Fatalf("classname=%q, want flow_a", c.Classname)
		}
	}
	c, ok := byName["Клиент создаёт заказ → статус NEW"]
	if !ok || !c.fail {
		t.Fatalf("registry title missing or no failure: %+v", byName)
	}
	if c, ok := byName["Ресторан начинает готовку → PREPARING"]; !ok || !c.skip {
		t.Fatalf("skipped case missing under registry title: %+v", byName)
	}
}

func TestJUnitXMLCustomSuiteFallsBackToKeys(t *testing.T) {
	run := testRun()
	run.SuiteKey = "my_custom"
	run.SuiteName = "my_custom"
	run.Results = map[string]runner.CheckResult{
		"step_1": {Status: runner.CheckPassed, DurationMs: 10},
		"step_2": {Status: runner.CheckSkipped, Message: "причина"},
	}
	data := JUnitXML(run)
	s := string(data)
	if !strings.Contains(s, `<testcase name="step_1" classname="my_custom"`) {
		t.Fatalf("custom check must use its ID as name:\n%s", s)
	}
	if !strings.Contains(s, `<skipped message="причина"/>`) {
		t.Fatalf("skipped element missing:\n%s", s)
	}
}

func TestAllureResultsZip(t *testing.T) {
	data, err := AllureResultsZip(testRun())
	if err != nil {
		t.Fatalf("AllureResultsZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	if len(zr.File) < 3 {
		t.Fatalf("zip entries=%d, want >= 3", len(zr.File))
	}

	found := map[string]bool{}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "-result.json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		var res struct {
			UUID      string `json:"uuid"`
			HistoryID string `json:"historyId"`
			Name      string `json:"name"`
			Status    string `json:"status"`
			Stage     string `json:"stage"`
			Start     int64  `json:"start"`
			Stop      int64  `json:"stop"`
			FullName  string `json:"fullName"`
			Labels    []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"labels"`
			StatusDetails *struct {
				Message string `json:"message"`
			} `json:"statusDetails"`
		}
		err = json.NewDecoder(rc).Decode(&res)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("%s is not valid JSON: %v", f.Name, err)
		}
		if f.Name != res.UUID+"-result.json" || len(res.UUID) != 36 {
			t.Fatalf("bad uuid/file pair: %s / %q", f.Name, res.UUID)
		}
		if res.Stage != "finished" || res.Start <= 0 || res.Stop < res.Start {
			t.Fatalf("bad timing/stage in %s: %+v", f.Name, res)
		}
		found[res.HistoryID] = true
		parentOK := false
		for _, l := range res.Labels {
			if l.Name == "parentSuite" && l.Value == "E2E" {
				parentOK = true
			}
		}
		if !parentOK {
			t.Fatalf("%s: parentSuite label missing", f.Name)
		}
		switch res.HistoryID {
		case "flow_a-create_order":
			if res.Status != "failed" || res.StatusDetails == nil ||
				res.StatusDetails.Message != `broken <a> & "b"` {
				t.Fatalf("create_order result wrong: %+v", res)
			}
			if res.Stop-res.Start != 250 {
				t.Fatalf("duration mismatch: start=%d stop=%d", res.Start, res.Stop)
			}
		case "flow_a-cooking":
			if res.Status != "skipped" || res.StatusDetails != nil {
				t.Fatalf("skipped must have no statusDetails: %+v", res)
			}
		case "flow_a-setup":
			if res.Status != "passed" {
				t.Fatalf("setup status=%s", res.Status)
			}
		}
	}
	for _, want := range []string{"flow_a-setup", "flow_a-create_order", "flow_a-cooking"} {
		if !found[want] {
			t.Fatalf("missing historyId %s", want)
		}
	}
}

func TestNotifyFailureNoopWithoutConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("no-op notify panicked: %v", r)
		}
	}()
	NotifyFailure(context.Background(), &config.Config{}, testRun())
	NotifyFailure(context.Background(), nil, nil)
}

func TestSendDigestNoopWithoutToken(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("no-op digest panicked: %v", r)
		}
	}()
	SendDigest(context.Background(), &config.Config{}, "text")
	SendDigest(context.Background(), nil, "text")
}

func TestDigestTextAllGreen(t *testing.T) {
	ok := func(key string) *runner.TestRun {
		return &runner.TestRun{SuiteKey: key, Status: "PASSED"}
	}
	got := DigestText([]*runner.TestRun{ok("flow_a"), ok("flow_b")}, "http://stage.locali.ru:8080")

	for _, want := range []string{
		"🚀 <b>Регресс завершён</b>",
		"Стенд: stage.locali.ru:8080",
		"Итог: ✅ 2/2 сютов зелёные",
		"Все сюты пройдены 🎉",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("digest %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "❌") || strings.Contains(got, "упал чек") {
		t.Fatalf("all-green digest must contain no failure lines:\n%s", got)
	}
}

func TestDigestTextListsFirstFailedCheck(t *testing.T) {
	long := strings.Repeat("ж", 200)
	failed := testRun() // flow_a, FAILED, create_order failed
	failed.Results["create_order"] = runner.CheckResult{
		Status:  runner.CheckFailed,
		Message: long,
	}
	green := &runner.TestRun{SuiteKey: "flow_b", Status: "PASSED"}

	got := DigestText([]*runner.TestRun{failed, green}, "")

	if !strings.Contains(got, "Итог: ✅ 1/2 сютов зелёные") {
		t.Fatalf("bad summary line:\n%s", got)
	}
	title := "Клиент создаёт заказ → статус NEW"
	if !strings.Contains(got, "❌ flow_a — упал чек «"+title+"»") {
		t.Fatalf("failed suite line missing:\n%s", got)
	}
	msgStart := strings.Index(got, long[:20])
	if msgStart < 0 {
		t.Fatalf("check message missing:\n%s", got)
	}
	// «(» closes the quoted reason; between them sits the truncated message.
	rest := got[msgStart:]
	end := strings.Index(rest, ")")
	truncated := []rune(rest[:end])
	if len(truncated) > maxDigestCheckMsgRunes {
		t.Fatalf("message not truncated: %d runes", len(truncated))
	}
	if strings.Contains(got, "Все сюты пройдены") {
		t.Fatalf("celebration line must be absent on failures:\n%s", got)
	}
}

func TestDigestTextEscapingAndErrorFallback(t *testing.T) {
	run := testRun()
	run.SuiteKey = "my_custom"
	run.SuiteName = "my_custom"
	run.Results = nil // no per-check data → fall back to run.Error

	got := DigestText([]*runner.TestRun{run}, "")

	if !strings.Contains(got, "❌ my_custom — ошибка: boom &lt;tag&gt; &amp; &#34;quote&#34;") &&
		!strings.Contains(got, "boom &lt;tag&gt;") {
		t.Fatalf("escaped run error missing:\n%s", got)
	}
}
