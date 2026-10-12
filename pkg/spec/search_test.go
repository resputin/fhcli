package spec

import (
	"reflect"
	"testing"
	"time"
)

func resultIDs(rs []Result) []string {
	ids := make([]string, len(rs))
	for i, r := range rs {
		ids[i] = r.Operation.ID
	}
	return ids
}

func TestTokenize(t *testing.T) {
	got := tokenize("List the Severity_Matrix impacts for /v1/nunc-connections/{incident_id}")
	want := []string{"list", "severity", "matrix", "impact", "v1", "nunc", "connection", "incident", "id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokenize = %v, want %v", got, want)
	}
}

func TestSingularize(t *testing.T) {
	for in, want := range map[string]string{
		"pages": "page", "policies": "policy", "impacts": "impact",
		"status": "status", "process": "process", "matrix": "matrix", "is": "is",
	} {
		if got := singularize(in); got != want {
			t.Errorf("singularize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchFindsBySummaryWords(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("close incident", 10))

	if len(got) == 0 || got[0] != "close_incident" {
		t.Errorf("results = %v, want close_incident first", got)
	}
}

func TestSearchPrefersOperationsMatchingMoreQueryWords(t *testing.T) {
	s := loadFixture(t)

	// "role" appears only in the role operations; "incident" is everywhere.
	got := resultIDs(s.Search("incident role", 10))

	if len(got) < 2 || !(got[0] == "get_incident_role" || got[0] == "delete_incident_role") ||
		!(got[1] == "get_incident_role" || got[1] == "delete_incident_role") {
		t.Errorf("results = %v, want both incident role operations first", got)
	}
}

func TestSearchMatchesParameterNames(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("milestone", 10))

	if !reflect.DeepEqual(got, []string{"close_incident"}) {
		t.Errorf("results = %v, want only close_incident (has a milestone body field)", got)
	}
}

func TestSearchMatchesTag(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("metrics", 10))

	if !reflect.DeepEqual(got, []string{"get_mttx"}) {
		t.Errorf("results = %v, want get_mttx via its Metrics/Reporting tag", got)
	}
}

func TestSearchIgnoresStopWordsAndCase(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("Close AN Incident", 1))

	if !reflect.DeepEqual(got, []string{"close_incident"}) {
		t.Errorf("results = %v", got)
	}
}

func TestSearchLimit(t *testing.T) {
	s := loadFixture(t)

	if got := s.Search("incident", 2); len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestSearchNoMatches(t *testing.T) {
	s := loadFixture(t)

	if got := s.Search("kubernetes", 10); len(got) != 0 {
		t.Errorf("results = %v, want none", resultIDs(got))
	}
	if got := s.Search("the a of", 10); len(got) != 0 {
		t.Errorf("a query of only stop words should match nothing, got %v", resultIDs(got))
	}
}

func TestSearchIsDeterministic(t *testing.T) {
	s := loadFixture(t)

	first := resultIDs(s.Search("incident", 10))
	for i := 0; i < 20; i++ {
		if got := resultIDs(s.Search("incident", 10)); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d = %v, first = %v", i, got, first)
		}
	}
}

func TestSearchScoresAreDescending(t *testing.T) {
	s := loadFixture(t)

	rs := s.Search("incident", 10)
	for i := 1; i < len(rs); i++ {
		if rs[i].Score > rs[i-1].Score {
			t.Errorf("score[%d]=%v > score[%d]=%v", i, rs[i].Score, i-1, rs[i-1].Score)
		}
	}
}

func loadEmbeddedSpec(t *testing.T) *Spec {
	t.Helper()
	s, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func containsAll(got []string, want ...string) []string {
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	var missing []string
	for _, id := range want {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func TestSearchRealSpecSeverityMatrix(t *testing.T) {
	s := loadEmbeddedSpec(t)

	got := resultIDs(s.Search("severity matrix", 10))

	if missing := containsAll(got, "get_severity_matrix", "update_severity_matrix", "create_severity_matrix_impact", "list_severity_matrix_conditions"); len(missing) > 0 {
		t.Errorf("top 10 = %v, missing %v", got, missing)
	}
	if got[0] != "get_severity_matrix" && got[0] != "update_severity_matrix" {
		t.Errorf("first = %s, want the matrix itself (get/update) ahead of its sub-resources", got[0])
	}
}

func TestSearchRealSpecStatusPageComponents(t *testing.T) {
	s := loadEmbeddedSpec(t)

	got := resultIDs(s.Search("status page component", 5))

	if missing := containsAll(got, "create_nunc_component_group", "update_nunc_component_group", "delete_nunc_component_group"); len(missing) > 0 {
		t.Errorf("top 5 = %v, missing %v", got, missing)
	}
}

func TestSearchRealSpecIsFast(t *testing.T) {
	s := loadEmbeddedSpec(t)
	start := time.Now()

	for i := 0; i < 10; i++ {
		s.Search("status page component", 10)
	}

	if per := time.Since(start) / 10; per > 50*time.Millisecond {
		t.Errorf("search took %v per query, want < 50ms", per)
	}
}

// A "[DEPRECATED]" marker adds a word to the summary that no query matches,
// which lowers the summary-focus tie-break below the replacement's.
func TestSearchRanksDeprecatedOperationsLast(t *testing.T) {
	s := &Spec{Operations: []Operation{
		{ID: "a_get_on_call_shift", Tag: "Teams", Summary: "[DEPRECATED] Get an on-call shift"},
		{ID: "b_get_on_call_shift", Tag: "Teams", Summary: "Get an on-call shift"},
	}}

	got := resultIDs(s.Search("on call shift", 10))

	if !reflect.DeepEqual(got, []string{"b_get_on_call_shift", "a_get_on_call_shift"}) {
		t.Errorf("results = %v, want the deprecated operation after its replacement", got)
	}
}
