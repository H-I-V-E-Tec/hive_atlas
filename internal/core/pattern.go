package core

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Go's RE2 \b is ASCII-only. The library's edge boundaries retain Python's
// Unicode word semantics so prose next to a signal does not introduce a lead.
type pattern struct {
	rx          *regexp.Regexp
	left, right bool
}

func compilePattern(raw string) (*pattern, error) {
	p := &pattern{left: strings.HasPrefix(raw, `\b`), right: strings.HasSuffix(raw, `\b`)}
	if p.left {
		raw = strings.TrimPrefix(raw, `\b`)
	}
	if p.right {
		raw = strings.TrimSuffix(raw, `\b`)
	}
	rx, err := regexp.Compile("(?i)" + raw)
	if err != nil {
		return nil, err
	}
	rx.Longest()
	p.rx = rx
	return p, nil
}
func wordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }
func (p *pattern) MatchString(text string) bool {
	for _, at := range p.rx.FindAllStringIndex(text, -1) {
		if p.left {
			left := false
			if at[0] > 0 {
				r, _ := utf8.DecodeLastRuneInString(text[:at[0]])
				left = wordRune(r)
			}
			r, _ := utf8.DecodeRuneInString(text[at[0]:])
			if left == wordRune(r) {
				continue
			}
		}
		if p.right {
			right := false
			if at[1] < len(text) {
				r, _ := utf8.DecodeRuneInString(text[at[1]:])
				right = wordRune(r)
			}
			r, _ := utf8.DecodeLastRuneInString(text[:at[1]])
			if right == wordRune(r) {
				continue
			}
		}
		return true
	}
	return false
}
