// Package hints provides fuzzy step-suggestion logic for unknown DSL steps.
package hints

import "strings"

// Suggest returns the closest known pattern to text, or "" if nothing is
// close enough. Comparison is done on normalised strings (lowercase, quoted
// values replaced by placeholders) using Levenshtein distance.
func Suggest(text string, patterns []string) string {
	norm := normalize(text)
	best := ""
	bestDist := len(norm) + 1
	for _, p := range patterns {
		d := levenshtein(norm, normalize(p))
		if d < bestDist {
			bestDist = d
			best = p
		}
	}
	threshold := len(norm) / 3
	if threshold < 3 {
		threshold = 3
	}
	if bestDist <= threshold {
		return best
	}
	return ""
}

// normalize lowercases the string and replaces quoted values with a
// placeholder so that differences in literal values don't skew the score.
func normalize(s string) string {
	s = strings.ToLower(s)
	s = replaceQuoted(s, '"')
	s = replaceQuoted(s, '\'')
	return strings.Join(strings.Fields(s), " ")
}

func replaceQuoted(s string, quote byte) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		if s[i] == quote {
			if !in {
				b.WriteString("<v>")
				in = true
			} else {
				in = false
			}
			continue
		}
		if in {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
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
			if ra[i-1] == rb[j-1] {
				curr[j] = prev[j-1]
			} else {
				m := prev[j-1]
				if prev[j] < m {
					m = prev[j]
				}
				if curr[j-1] < m {
					m = curr[j-1]
				}
				curr[j] = m + 1
			}
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
