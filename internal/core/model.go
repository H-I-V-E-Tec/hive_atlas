package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Condition struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"`
	Means   string `json:"means"`
	Role    string `json:"role"`
	rx      *pattern
}

type Ficha struct {
	ID                 string      `json:"id"`
	Version            int         `json:"version"`
	State              string      `json:"state"`
	Kind               string      `json:"kind"`
	SignalClass        string      `json:"signal_class"`
	Mechanism          string      `json:"mechanism"`
	Patterns           []string    `json:"patterns"`
	Conditions         []Condition `json:"conditions"`
	Feeds              []string    `json:"feeds"`
	Requires           []string    `json:"requires"`
	Confirmation       string      `json:"confirmation"`
	Counterexample     string      `json:"counterexample"`
	History            []string    `json:"historico,omitempty"`
	EmbeddingModel     string      `json:"embedding_model,omitempty"`
	EmbeddingDimension int         `json:"embedding_dimension,omitempty"`
	pats               []*pattern
}

type Library struct {
	SchemaVersion int     `json:"schema_version"`
	ProducedBy    string  `json:"produced_by,omitempty"`
	Signals       []Ficha `json:"signals"`
}

func Load(data []byte) (*Library, error) {
	var lib Library
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("invalid library JSON: %w", err)
	}
	if err := lib.Compile(); err != nil {
		return nil, err
	}
	return &lib, nil
}

func (lib *Library) Compile() error {
	if lib.SchemaVersion != 1 {
		return fmt.Errorf("unsupported library schema_version: %d", lib.SchemaVersion)
	}
	seen := map[string]bool{}
	for i := range lib.Signals {
		f := &lib.Signals[i]
		if f.Patterns == nil {
			f.Patterns = []string{}
		}
		if f.Conditions == nil {
			f.Conditions = []Condition{}
		}
		if f.Feeds == nil {
			f.Feeds = []string{}
		}
		if f.Requires == nil {
			f.Requires = []string{}
		}
		if f.ID == "" || seen[f.ID] {
			return fmt.Errorf("empty or duplicate signal id: %q", f.ID)
		}
		seen[f.ID] = true
		if f.Version < 1 || f.Mechanism == "" {
			return fmt.Errorf("signal %s requires version and mechanism", f.ID)
		}
		if f.State != "revisada" && f.State != "ativa" {
			return fmt.Errorf("signal %s is not reviewed/active", f.ID)
		}
		if f.Kind != "signal" && f.Kind != "chain" {
			return fmt.Errorf("invalid kind for %s", f.ID)
		}
		if f.Kind == "chain" && len(f.Requires) == 0 {
			return fmt.Errorf("chain %s has no requires", f.ID)
		}
		f.pats = nil
		for _, pattern := range f.Patterns {
			rx, err := compilePattern(pattern)
			if err != nil {
				return fmt.Errorf("signal %s: unsupported RE2 pattern: %w", f.ID, err)
			}
			f.pats = append(f.pats, rx)
		}
		for j := range f.Conditions {
			c := &f.Conditions[j]
			if c.Role == "" {
				c.Role = "required"
			}
			if c.Role != "required" && c.Role != "refute" {
				return fmt.Errorf("invalid condition role in %s", f.ID)
			}
			rx, err := compilePattern(c.Pattern)
			if err != nil {
				return fmt.Errorf("condition %s: unsupported RE2 pattern: %w", c.ID, err)
			}
			c.rx = rx
		}
	}
	for _, f := range lib.Signals {
		for _, id := range f.Requires {
			if !seen[id] {
				return fmt.Errorf("signal %s requires missing signal %s", f.ID, id)
			}
		}
	}
	return nil
}

type Evidence struct {
	SchemaVersion int    `json:"schema_version,omitempty"`
	Kind          string `json:"kind"`
	Value         string `json:"value,omitempty"`
	ProgramID     string `json:"program_id,omitempty"`
	Asset         string `json:"asset,omitempty"`
	Flow          string `json:"flow,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Method        string `json:"method,omitempty"`
	Role          string `json:"role,omitempty"`
	Object        string `json:"object,omitempty"`
	Ref           string `json:"ref,omitempty"`
}

func (e Evidence) Validate() error {
	if e.SchemaVersion != 0 && e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported evidence schema_version")
	}
	switch e.Kind {
	case "endpoint", "param", "header", "js_finding", "nota":
	default:
		return fmt.Errorf("invalid evidence kind")
	}
	data, _ := json.Marshal(e)
	if HasSecret(string(data)) {
		return fmt.Errorf("evidence contains credentials; sanitize at the producer")
	}
	return nil
}

func (e Evidence) Text() string {
	parts := []string{}
	for _, s := range []string{e.Value, e.Endpoint, e.Flow, e.Object, e.Method, e.Role} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization\s*[:=]\s*(bearer|basic)\s+\S+`),
	regexp.MustCompile(`(?i)\b(cookie|set-cookie)\s*:\s*\S+`),
	regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[A-Z0-9]{16}|sk-[A-Za-z0-9_-]{20,})\b`),
	regexp.MustCompile(`(?i)"(authorization|cookie|set-cookie|password|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)"\s*:\s*"[^"\s][^"]*"`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password)\s*[=:]\s*[^\s&"<>]+`),
}

func HasSecret(s string) bool {
	// JSON escapes are normalized so headers inside JSON strings are inspected too.
	s = strings.ReplaceAll(s, `\"`, `"`)
	for _, rx := range secretPatterns {
		if rx.MatchString(s) {
			return true
		}
	}
	// Inspect decoded JSON too: escaped header names/whitespace must not bypass
	// the adapter's filter. Never include rejected content in an error or log.
	var decoded any
	if json.Unmarshal([]byte(s), &decoded) == nil {
		var walk func(any) bool
		walk = func(value any) bool {
			switch v := value.(type) {
			case string:
				for _, rx := range secretPatterns {
					if rx.MatchString(v) {
						return true
					}
				}
			case []any:
				for _, item := range v {
					if walk(item) {
						return true
					}
				}
			case map[string]any:
				for key, item := range v {
					if text, ok := item.(string); ok {
						for _, rx := range secretPatterns {
							if rx.MatchString(key + ": " + text) {
								return true
							}
						}
					}
					if walk(item) {
						return true
					}
				}
			}
			return false
		}
		return walk(decoded)
	}
	return false
}
