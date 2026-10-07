package core

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

type Hit struct {
	ID        string  `json:"id"`
	Score     float64 `json:"score"`
	Mechanism string  `json:"mechanism"`
}

var word = regexp.MustCompile(`[a-z0-9_]{2,}`)

func terms(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range word.FindAllString(strings.ToLower(s), -1) {
		if t != "redirect" {
			out[t] = true
		}
	}
	return out
}
func FichaText(f Ficha) string {
	texts := append([]string{f.Mechanism, f.Counterexample}, f.Patterns...)
	for _, c := range f.Conditions {
		texts = append(texts, c.Means)
	}
	bag := terms(strings.Join(texts, " "))
	keys := []string{}
	for t := range bag {
		keys = append(keys, t)
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}
func (lib *Library) Search(query string, k int) []Hit {
	q := terms(query)
	hits := []Hit{}
	for _, f := range lib.Signals {
		doc := terms(FichaText(f))
		overlap := 0
		for t := range q {
			if doc[t] {
				overlap++
			}
		}
		if overlap > 0 {
			hits = append(hits, Hit{ID: f.ID, Score: float64(overlap) / math.Sqrt(float64(len(doc))), Mechanism: f.Mechanism})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].ID < hits[j].ID
		}
		return hits[i].Score > hits[j].Score
	})
	if k < len(hits) {
		hits = hits[:k]
	}
	return hits
}
