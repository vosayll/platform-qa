package report

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"locali-e2e-engine/pkg/runner"
)

// Allure label names per the Allure result schema.
const (
	labelSuite       = "suite"
	labelParentSuite = "parentSuite"
	labelStory       = "story"
	labelPackage     = "package"

	parentSuiteValue = "E2E"
	packageValue     = "locali.e2e"
)

type allureLabel struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type allureStatusDetails struct {
	Message string `json:"message,omitempty"`
	Trace   string `json:"trace,omitempty"`
}

type allureResult struct {
	UUID          string               `json:"uuid"`
	HistoryID     string               `json:"historyId"`
	Name          string               `json:"name"`
	Status        string               `json:"status"`
	Stage         string               `json:"stage"`
	Start         int64                `json:"start"`
	Stop          int64                `json:"stop"`
	FullName      string               `json:"fullName"`
	Labels        []allureLabel        `json:"labels"`
	StatusDetails *allureStatusDetails `json:"statusDetails,omitempty"`
	Attachments   []struct{}           `json:"attachments"`
	Parameters    []struct{}           `json:"parameters"`
	Steps         []struct{}           `json:"steps"`
}

// AllureResultsZip packs the run's check results as an Allure results ZIP:
// one <uuid>-result.json per check, following the Allure 2 result schema.
func AllureResultsZip(run *runner.TestRun) ([]byte, error) {
	entries := orderedEntries(run)

	base := time.Now().UnixMilli()
	if run != nil && !run.StartTime.IsZero() {
		base = run.StartTime.UnixMilli()
	}

	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	cursor := base
	for _, e := range entries {
		res := buildResult(e, cursor)
		cursor += e.DurationMs
		if res.Stop < res.Start {
			res.Stop = res.Start
		}

		data, err := json.Marshal(res)
		if err != nil {
			_ = w.Close()
			return nil, err
		}
		f, err := w.Create(res.UUID + "-result.json")
		if err != nil {
			_ = w.Close()
			return nil, err
		}
		if _, err := f.Write(data); err != nil {
			_ = w.Close()
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildResult(e entry, startMs int64) allureResult {
	status := "skipped"
	switch e.Status {
	case runner.CheckPassed:
		status = "passed"
	case runner.CheckFailed:
		status = "failed"
	}

	stopMs := startMs + e.DurationMs

	res := allureResult{
		UUID:        uuid.New().String(),
		HistoryID:   e.SuiteKey + "-" + e.CheckID,
		Name:        e.Title,
		Status:      status,
		Stage:       "finished",
		Start:       startMs,
		Stop:        stopMs,
		FullName:    e.SuiteKey + "/" + e.CheckID,
		Labels:      allureLabels(e),
		Attachments: []struct{}{},
		Parameters:  []struct{}{},
		Steps:       []struct{}{},
	}
	if status == "failed" {
		res.StatusDetails = &allureStatusDetails{Message: e.Message}
	}
	return res
}

func allureLabels(e entry) []allureLabel {
	return []allureLabel{
		{Name: labelSuite, Value: e.SuiteKey},
		{Name: labelParentSuite, Value: parentSuiteValue},
		{Name: labelStory, Value: e.Title},
		{Name: labelPackage, Value: packageValue},
	}
}
