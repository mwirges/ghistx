// Package fuzzy implements Levenshtein edit distance and phrase matching
// for post-filtering history results.
package fuzzy

import (
	"strings"

	"github.com/mwirges/ghistx/internal/find"
)

// Distance returns the Levenshtein edit distance between a and b.
func Distance(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// threshold returns the allowed edit distance for a query word of length n.
// Starts at 1 for short words; grows by 1 per 5 additional characters.
func threshold(n int) int {
	t := n / 5
	if t < 1 {
		t = 1
	}
	return t
}

// MatchPhrase reports whether phrase appears in cmd, and whether the match is exact.
//
// Exact: cmd contains phrase as a case-insensitive contiguous substring.
// Fuzzy: the words of phrase each appear in cmd (split on whitespace and common
// shell delimiters), in order, with each query word within edit-distance threshold
// of a cmd token or as a substring of one.
func MatchPhrase(cmd, phrase string) (matched, exact bool) {
	cmdLow := strings.ToLower(cmd)
	phraseLow := strings.ToLower(strings.TrimSpace(phrase))
	if phraseLow == "" {
		return false, false
	}

	if strings.Contains(cmdLow, phraseLow) {
		return true, true
	}

	// Fuzzy: walk cmd tokens left-to-right, matching query words in sequence.
	queryWords := strings.Fields(phraseLow)
	cmdTokens := strings.FieldsFunc(cmdLow, isDelimiter)

	qi := 0
	for _, tok := range cmdTokens {
		if qi >= len(queryWords) {
			break
		}
		qw := queryWords[qi]
		if strings.Contains(tok, qw) || Distance(tok, qw) <= threshold(len(qw)) {
			qi++
		}
	}
	if qi == len(queryWords) {
		return true, false
	}
	return false, false
}

func isDelimiter(r rune) bool {
	switch r {
	case ' ', '\t', '/', '=', ':', ',', ';', '|', '&', '(', ')', '[', ']', '{', '}', '<', '>':
		return true
	}
	return false
}

// FilterPhrase returns hits that match phrase, with exact matches first.
// Returns nil when no hits match.
func FilterPhrase(hits []find.Hit, phrase string) []find.Hit {
	var exact, fuzzyHits []find.Hit
	for _, h := range hits {
		matched, isExact := MatchPhrase(h.Cmd, phrase)
		if !matched {
			continue
		}
		if isExact {
			exact = append(exact, h)
		} else {
			fuzzyHits = append(fuzzyHits, h)
		}
	}
	return append(exact, fuzzyHits...)
}
