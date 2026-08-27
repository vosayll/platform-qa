package report

import (
	"fmt"
	"strings"

	"locali-e2e-engine/pkg/runner"
)

// JUnitXML renders a finished run as a standard JUnit XML document
// (<testsuites>/<testsuite>/<testcase>). Check titles come from the suite
// registry; checks unknown to it (custom scenarios) use their IDs as names.
// All dynamic values are XML-escaped.
func JUnitXML(run *runner.TestRun) []byte {
	entries := orderedEntries(run)

	type suiteOut struct {
		key      string
		title    string
		cases    []entry
		failures int
		skipped  int
		seconds  float64
	}

	var suites []*suiteOut
	var cur *suiteOut
	for _, e := range entries {
		if cur == nil || cur.key != e.SuiteKey {
			cur = &suiteOut{key: e.SuiteKey, title: suiteDisplayName(run, e.SuiteKey)}
			suites = append(suites, cur)
		}
		switch e.Status {
		case runner.CheckFailed:
			cur.failures++
		case runner.CheckSkipped:
			cur.skipped++
		}
		cur.seconds += float64(e.DurationMs) / 1000.0
		cur.cases = append(cur.cases, e)
	}

	totalFailures, totalSeconds := 0, 0.0
	for _, su := range suites {
		totalFailures += su.failures
		totalSeconds += su.seconds
	}

	var b strings.Builder
	b.WriteString(xmlHeader)
	fmt.Fprintf(&b, "<testsuites tests=\"%d\" failures=\"%d\" time=\"%s\">\n",
		len(entries), totalFailures, fmtSeconds(totalSeconds))
	for _, su := range suites {
		fmt.Fprintf(&b, "  <testsuite name=\"%s\" tests=\"%d\" failures=\"%d\" skipped=\"%d\" time=\"%s\">\n",
			xmlEscape(su.title), len(su.cases), su.failures, su.skipped, fmtSeconds(su.seconds))

		for _, c := range su.cases {
			name, class, dur := xmlEscape(c.Title), xmlEscape(c.SuiteKey), fmtSeconds(float64(c.DurationMs)/1000.0)
			switch c.Status {
			case runner.CheckFailed:
				msg := xmlEscape(c.Message)
				fmt.Fprintf(&b, "    <testcase name=\"%s\" classname=\"%s\" time=\"%s\">\n", name, class, dur)
				fmt.Fprintf(&b, "      <failure message=\"%s\">%s</failure>\n", msg, msg)
				b.WriteString("    </testcase>\n")
			case runner.CheckSkipped:
				fmt.Fprintf(&b, "    <testcase name=\"%s\" classname=\"%s\" time=\"%s\">\n", name, class, dur)
				fmt.Fprintf(&b, "      <skipped message=\"%s\"/>\n", xmlEscape(c.Message))
				b.WriteString("    </testcase>\n")
			default:
				fmt.Fprintf(&b, "    <testcase name=\"%s\" classname=\"%s\" time=\"%s\"/>\n", name, class, dur)
			}
		}
		b.WriteString("  </testsuite>\n")
	}
	b.WriteString("</testsuites>\n")
	return []byte(b.String())
}

func fmtSeconds(sec float64) string {
	return fmt.Sprintf("%.3f", sec)
}

const xmlHeader = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"
