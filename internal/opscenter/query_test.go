package opscenter

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSelectorQuotesValuesSoTheyCannotEscapeTheMatcher(t *testing.T) {
	sel, err := Selector("up", []Matcher{{Name: "job", Op: "=", Value: `x"} or vector(1) or up{a="`}})
	if err != nil {
		t.Fatal(err)
	}
	want := `up{job="x\"} or vector(1) or up{a=\""}`
	if sel != want {
		t.Fatalf("selector = %s, want %s", sel, want)
	}
}

func TestSelectorRejectsInvalidNamesAndRegex(t *testing.T) {
	cases := []struct {
		metric string
		m      Matcher
	}{
		{"up", Matcher{Name: "job}", Op: "=", Value: "a"}},
		{"up) or (x", Matcher{Name: "job", Op: "=", Value: "a"}},
		{"up", Matcher{Name: "job", Op: "=~", Value: "(unclosed"}},
		{"up", Matcher{Name: "job", Op: "==", Value: "a"}},
	}
	for _, c := range cases {
		if _, err := Selector(c.metric, []Matcher{c.m}); err == nil {
			t.Fatalf("expected error for %+v", c)
		}
	}
}

func TestLogFilterBuildsEscapedLogQL(t *testing.T) {
	q, err := LogFilter{Services: []string{"platform-api", "backup.service"}, Levels: []string{"error"}, Keyword: `boom" | line_format "x`, Exclude: "health"}.LogQL()
	if err != nil {
		t.Fatal(err)
	}
	want := `{service_name=~"backup\\.service|platform-api",level="error"} |= "boom\" | line_format \"x" != "health"`
	if q != want {
		t.Fatalf("logql = %s\nwant    %s", q, want)
	}
	all, _ := LogFilter{}.LogQL()
	if all != `{service_name=~".+"}` {
		t.Fatalf("empty filter = %s", all)
	}
	if _, err := (LogFilter{Levels: []string{"verbose"}}).LogQL(); err == nil {
		t.Fatal("unknown level must be rejected")
	}
	if _, err := (LogFilter{Keyword: "(", Regex: true}).LogQL(); err == nil {
		t.Fatal("invalid regex keyword must be rejected")
	}
}

func TestExploreQueryWhitelist(t *testing.T) {
	q, err := ExploreQuery("http_requests_total", []Matcher{{Name: "code", Op: "=~", Value: "5.."}}, "sum_rate", "5m", "job,instance")
	if err != nil {
		t.Fatal(err)
	}
	if q != `sum by (job,instance)(rate(http_requests_total{code=~"5.."}[5m]))` {
		t.Fatalf("query = %s", q)
	}
	for _, bad := range [][3]string{{"up", "label_replace", "5m"}, {"up", "rate", "7m"}} {
		if _, err := ExploreQuery(bad[0], nil, bad[1], bad[2], ""); err == nil {
			t.Fatalf("expected rejection for %v", bad)
		}
	}
	if _, err := ExploreQuery("up", nil, "sum", "5m", "job) or (x"); err == nil {
		t.Fatal("invalid grouping label must be rejected")
	}
}

func TestStreamSelectorSortsAndQuotes(t *testing.T) {
	sel, err := StreamSelector(map[string]string{"service_name": "api", "level": `in"fo`})
	if err != nil {
		t.Fatal(err)
	}
	if sel != `{level="in\"fo",service_name="api"}` {
		t.Fatalf("selector = %s", sel)
	}
}

func TestTimeRangeLimits(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := TimeRange(now.Add(-48*time.Hour), now, 24*time.Hour, now); err == nil {
		t.Fatal("range above maximum must be rejected")
	}
	var v *ValidationError
	if _, _, err := TimeRange(now, now.Add(-time.Hour), 24*time.Hour, now); !errors.As(err, &v) {
		t.Fatal("reversed range must be a validation error")
	}
	start, end, err := TimeRange(time.Time{}, time.Time{}, 24*time.Hour, now)
	if err != nil || !end.Equal(now) || end.Sub(start) != time.Hour {
		t.Fatalf("default range = %v %v %v", start, end, err)
	}
}

func TestStepStaysWithinPointLimit(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(31 * 24 * time.Hour)
	step := Step(start, end, time.Second, 20000)
	if points := end.Sub(start) / step; points > 11000 {
		t.Fatalf("step %v yields %d points", step, points)
	}
}

func TestCheckQueryText(t *testing.T) {
	if err := checkQueryText("q", strings.Repeat("a", maxQueryLength+1)); err == nil {
		t.Fatal("oversized query must be rejected")
	}
	if err := checkQueryText("q", "  "); err == nil {
		t.Fatal("empty query must be rejected")
	}
}

func TestParseSelectorRoundTrip(t *testing.T) {
	want := []Matcher{{Name: "service_name", Op: "=", Value: `a"b}`}, {Name: "level", Op: "=~", Value: "error|warn"}, {Name: "env", Op: "!=", Value: "dev"}}
	selector, err := Selector("", want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSelector(selector)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSelector(%s) = %v, %v", selector, got, err)
	}
	if got, err := ParseSelector("{ app = `x` , job!~\"y.*\" }"); err != nil || len(got) != 2 || got[0].Value != "x" || got[1].Op != "!~" {
		t.Fatalf("spaced selector = %v, %v", got, err)
	}
	for _, bad := range []string{`app="x"`, `{app="x"} or {}`, `{app='x'}`, `{1app="x"}`, `{app="x" job="y"}`, `{app=~"("}`, `{app="x"`} {
		if _, err := ParseSelector(bad); err == nil {
			t.Errorf("ParseSelector(%q) should fail", bad)
		}
	}
}
