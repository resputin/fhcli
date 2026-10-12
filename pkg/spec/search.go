package spec

import (
	"sort"
	"strings"
	"unicode"
)

// Result is one operation matched by Search.
type Result struct {
	Operation Operation
	// Score orders results: higher is better. It grows with the number of
	// query words matched first, then with how strongly they matched.
	Score float64
}

// Field weights: a query word scores the best weight among the fields it
// appears in.
const (
	weightID          = 4
	weightSummary     = 3
	weightTag         = 3
	weightPath        = 2
	weightParam       = 1.5
	weightDescription = 1
)

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "for": true, "to": true, "of": true,
	"and": true, "or": true, "in": true, "on": true, "by": true, "with": true,
}

// Search ranks operations against a free-text query and returns at most
// limit results (all of them when limit <= 0). Operations matching more of
// the query's words rank first, then by match strength, then by how much of
// the summary the query covers, so "Get severity matrix" outranks "Delete a
// severity matrix condition" for "severity matrix".
func (s *Spec) Search(query string, limit int) []Result {
	words := uniq(tokenize(query))
	if len(words) == 0 {
		return nil
	}
	queryWords := make(map[string]bool, len(words))
	for _, w := range words {
		queryWords[w] = true
	}

	type ranked struct {
		Result
		index int
	}
	var hits []ranked
	for i, op := range s.Operations {
		fields := searchFields(op)
		matched, strength := 0, 0.0
		for _, w := range words {
			best := 0.0
			for _, f := range fields {
				if f.weight > best && f.words[w] {
					best = f.weight
				}
			}
			if best > 0 {
				matched++
				strength += best
			}
		}
		if matched == 0 {
			continue
		}
		focus := coverage(tokenize(op.Summary), queryWords)
		// Words matched dominate; strength and summary focus break ties.
		score := float64(matched)*100 + strength + focus
		hits = append(hits, ranked{Result{Operation: op, Score: score}, i})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.index < b.index
	})

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Result, len(hits))
	for i, h := range hits {
		out[i] = h.Result
	}
	return out
}

type searchField struct {
	weight float64
	words  map[string]bool
}

func searchFields(op Operation) []searchField {
	var params []string
	for _, group := range [][]Param{op.PathParams, op.QueryParams, op.BodyFields} {
		for _, p := range group {
			params = append(params, p.Name)
		}
	}
	return []searchField{
		{weightID, wordSet(op.ID)},
		{weightSummary, wordSet(op.Summary)},
		{weightTag, wordSet(op.Tag)},
		{weightPath, wordSet(op.Path)},
		{weightParam, wordSet(strings.Join(params, " "))},
		{weightDescription, wordSet(op.Description)},
	}
}

func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range tokenize(s) {
		set[w] = true
	}
	return set
}

// coverage is the fraction of distinct summary words that are query words.
func coverage(summary []string, query map[string]bool) float64 {
	distinct := uniq(summary)
	if len(distinct) == 0 {
		return 0
	}
	n := 0
	for _, w := range distinct {
		if query[w] {
			n++
		}
	}
	return float64(n) / float64(len(distinct))
}

// tokenize lowercases s, splits it on anything that isn't a letter or digit
// (so snake_case, kebab-case, paths and {params} break apart), drops stop
// words, and singularizes each word.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := fields[:0]
	for _, f := range fields {
		if stopWords[f] {
			continue
		}
		words = append(words, singularize(f))
	}
	return words
}

// singularize strips simple English plurals: "policies" → "policy",
// "pages" → "page". Words ending in "ss", "us" or "is" are left alone
// ("process", "status", "analysis").
func singularize(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && strings.HasSuffix(w, "s") &&
		!strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		return w[:len(w)-1]
	}
	return w
}

func uniq(words []string) []string {
	seen := make(map[string]bool, len(words))
	out := words[:0:0]
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
