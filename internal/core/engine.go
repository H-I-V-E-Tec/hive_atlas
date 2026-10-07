package core

import (
	"fmt"
	"sort"
	"strings"
)

type Recommendation struct {
	ID          string   `json:"id"`
	Version     int      `json:"version"`
	Verdict     string   `json:"verdict"`
	Present     []string `json:"present"`
	Absent      []string `json:"absent"`
	Unknown     []string `json:"unknown"`
	State       string   `json:"state"`
	Observation string   `json:"observation"`
	Ref         string   `json:"ref"`
	ficha       *Ficha
}

type Board struct {
	Board string           `json:"board"`
	Leads []Recommendation `json:"leads"`
}

func matches(c Condition, events []Evidence) bool {
	for _, e := range events {
		if c.rx.MatchString(e.Text()) {
			return true
		}
	}
	return false
}

func assess(f *Ficha, local, context []Evidence) Recommendation {
	r := Recommendation{ID: f.ID, Version: f.Version, Verdict: "forte", State: "UNTESTED", Present: []string{}, Absent: []string{}, Unknown: []string{}, ficha: f}
	for _, c := range f.Conditions {
		if c.Role == "refute" {
			if matches(c, local) {
				r.Absent = append(r.Absent, c.Means+" (contraexemplo presente)")
				r.Verdict = "descartado"
			}
		} else if len(context) == 0 {
			r.Unknown = append(r.Unknown, c.Means)
			if r.Verdict != "descartado" {
				r.Verdict = "candidato"
			}
		} else if matches(c, context) {
			r.Present = append(r.Present, c.Means)
		} else {
			r.Absent = append(r.Absent, c.Means)
			r.Verdict = "descartado"
		}
	}
	return r
}

func lead(r Recommendation) bool { return r.Verdict == "forte" || r.Verdict == "candidato" }
func better(r, prev Recommendation) bool {
	return prev.ID == "" || r.Verdict == "forte" && prev.Verdict != "forte"
}
func weight(f *Ficha) int {
	w, ok := map[string]int{"IDOR": 1, "BAC": 1, "LOGIC": 2, "HMAC": 3, "HDR": 4, "REDIR": 5, "OSINT": 6}[f.SignalClass]
	if !ok {
		return 9
	}
	return w
}

func (lib *Library) Recommend(events []Evidence, k int) Board {
	groups := map[[3]string][]Evidence{}
	keys := [][3]string{}
	for _, e := range events {
		key := [3]string{e.ProgramID, e.Asset, e.Flow}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], e)
	}
	best := map[string]Recommendation{}
	for _, key := range keys {
		sub := groups[key]
		var only map[string]bool
		if k > 0 {
			texts := []string{}
			for _, e := range sub {
				texts = append(texts, e.Text())
			}
			only = map[string]bool{}
			for _, h := range lib.Search(strings.Join(texts, " "), k) {
				only[h.ID] = true
			}
			initial := map[string]bool{}
			for id := range only {
				initial[id] = true
			}
			for _, f := range lib.Signals {
				if initial[f.ID] {
					for _, id := range append(append([]string{}, f.Requires...), f.Feeds...) {
						only[id] = true
					}
				}
			}
		}
		results := map[string]Recommendation{}
		for i := range lib.Signals {
			f := &lib.Signals[i]
			if f.Kind != "signal" || only != nil && !only[f.ID] {
				continue
			}
			for _, e := range sub {
				trigger := false
				for _, rx := range f.pats {
					if rx.MatchString(e.Text()) {
						trigger = true
						break
					}
				}
				if !trigger {
					continue
				}
				r := assess(f, []Evidence{e}, sub)
				r.Observation = e.Text()
				r.Ref = e.Ref
				prev := results[f.ID]
				if prev.ID == "" || lead(r) && !lead(prev) || better(r, prev) {
					results[f.ID] = r
				}
			}
		}
		for i := range lib.Signals {
			f := &lib.Signals[i]
			if f.Kind != "chain" || only != nil && !only[f.ID] {
				continue
			}
			valid := true
			inherited := false
			for _, id := range f.Requires {
				if !lead(results[id]) {
					valid = false
				}
				if results[id].Verdict == "candidato" {
					inherited = true
				}
			}
			if !valid {
				continue
			}
			r := assess(f, sub, sub)
			if !lead(r) {
				continue
			}
			if inherited && r.Verdict == "forte" {
				r.Verdict = "candidato"
			}
			anchor := results[f.Requires[0]]
			r.Observation = "encadeia " + strings.Join(f.Requires, ", ") + ": " + anchor.Observation
			r.Ref = anchor.Ref
			for _, id := range f.Requires {
				r.Present = append(r.Present, "feeder "+id+" disparou")
			}
			results[f.ID] = r
		}
		for id, r := range results {
			if lead(r) && better(r, best[id]) {
				best[id] = r
			}
		}
	}
	leads := []Recommendation{}
	for _, r := range best {
		leads = append(leads, r)
	}
	sort.Slice(leads, func(i, j int) bool {
		a, b := leads[i], leads[j]
		if a.ficha.Kind != b.ficha.Kind {
			return a.ficha.Kind == "chain"
		}
		if weight(a.ficha) != weight(b.ficha) {
			return weight(a.ficha) < weight(b.ficha)
		}
		if a.Verdict != b.Verdict {
			return a.Verdict == "forte"
		}
		return a.ID < b.ID
	})
	board := "sem leads: nenhum sinal com pré-condições sustentadas.  [UNTESTED]"
	if len(leads) > 0 {
		parts := []string{}
		for _, r := range leads {
			ref := r.Ref
			if ref == "" {
				ref = "—"
			}
			parts = append(parts, fmt.Sprintf("%s v%d  [%s]\n  observação: %s  (ref: %s)\n  por quê: %s\n  pré-condições: presentes=%v ausentes=%v desconhecidas=%v\n  contraexemplo: %s\n  confirmação sugerida: %s\n  estado: hipótese ainda não testada  [UNTESTED]", r.ID, r.Version, r.Verdict, r.Observation, ref, r.ficha.Mechanism, r.Present, r.Absent, r.Unknown, r.ficha.Counterexample, r.ficha.Confirmation))
		}
		board = strings.Join(parts, "\n\n")
	}
	return Board{Board: board, Leads: leads}
}
