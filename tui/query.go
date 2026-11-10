package tui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// The filter uses fzf's extended search syntax, with exact (not fuzzy)
// matching:
//
//	a b      both a and b
//	!a       not a
//	a | b    a or b; "|" binds tighter than a space
//	"a b"    the phrase "a b" (an addition to fzf)
//	^a  a$   the message starts / ends with a
//	'a       exact match; accepted for fzf habits, same as a
//
// Matching ignores case unless the term has an uppercase letter.

// term is one filter word.
type term struct {
	neg     bool
	re      *regexp.Regexp
	message bool // match the message only, without the line's time and stream
}

func (t term) match(plain string) bool {
	s := plain
	if t.message {
		_, _, s = splitPrefix(plain)
	}
	return t.re.MatchString(s) != t.neg
}

// query is a list of OR groups. An entry passes when every group has a
// matching term.
type query [][]term

func (q query) match(plain string) bool {
	for _, group := range q {
		ok := false
		for _, t := range group {
			if t.match(plain) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// parseQuery reads a filter in the syntax above. An empty filter returns a
// nil query.
func parseQuery(s string) (query, error) {
	words, err := splitWords(s)
	if err != nil {
		return nil, err
	}
	var q query
	join := false
	for _, w := range words {
		if w == "|" {
			if len(q) == 0 || join {
				return nil, fmt.Errorf(`"|" needs a term on each side`)
			}
			join = true
			continue
		}
		t, err := parseTerm(w)
		if err != nil {
			return nil, err
		}
		if join {
			q[len(q)-1] = append(q[len(q)-1], t)
			join = false
		} else {
			q = append(q, []term{t})
		}
	}
	if join {
		return nil, fmt.Errorf(`"|" needs a term on each side`)
	}
	return q, nil
}

// splitWords splits on spaces. Text in double quotes stays one word, and the
// quotes are kept so parseTerm can tell a quoted "|" or "!" from an operator.
func splitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inQuote, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
			started = true
		case unicode.IsSpace(r) && !inQuote:
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("missing closing quote")
	}
	if started {
		words = append(words, cur.String())
	}
	return words, nil
}

// parseTerm reads one word: [!][']['^]text[$], where text may be quoted.
func parseTerm(w string) (term, error) {
	var t term
	if strings.HasPrefix(w, "!") {
		t.neg, w = true, w[1:]
	}
	w = strings.TrimPrefix(w, "'")
	start := strings.HasPrefix(w, "^")
	if start {
		w = w[1:]
	}
	end := strings.HasSuffix(w, "$") && !strings.HasSuffix(w, `\$`)
	if end {
		w = w[:len(w)-1]
	}
	if len(w) >= 2 && strings.HasPrefix(w, `"`) && strings.HasSuffix(w, `"`) {
		w = w[1 : len(w)-1]
	}
	w = strings.ReplaceAll(w, `\$`, "$")
	if w == "" {
		return t, fmt.Errorf("empty term")
	}
	pattern := regexp.QuoteMeta(w)
	if start {
		pattern = `^\s*` + pattern
	}
	if end {
		pattern += `\s*$`
	}
	if !hasUpper(w) {
		pattern = "(?i)" + pattern
	}
	t.re = regexp.MustCompile(pattern)
	t.message = start || end
	return t, nil
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
