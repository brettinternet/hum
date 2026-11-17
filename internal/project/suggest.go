package project

import "strings"

// SuggestName returns the candidate closest to typed, or "" when nothing is
// close enough to be a plausible typo. A unique prefix match wins; otherwise
// the nearest candidate within an edit distance of 1 (2 for names of four or
// more characters, counting an adjacent swap as one edit) is returned. Callers never act on the suggestion.
func SuggestName(typed string, candidates []string) string {
	typed = strings.ToLower(typed)
	if typed == "" {
		return ""
	}
	limit := 1
	if len(typed) >= 4 {
		limit = 2
	}
	best, bestDistance := "", limit+1
	prefixMatches := 0
	prefixMatch := ""
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, typed) {
			prefixMatches++
			prefixMatch = candidate
		}
		if distance := editDistance(typed, candidate); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if prefixMatches == 1 {
		return prefixMatch
	}
	return best
}

// editDistance is the optimal string alignment distance: insertions,
// deletions, substitutions, and adjacent transpositions each cost one, so a
// swapped pair such as "wbe" for "web" is a single edit.
func editDistance(a, b string) int {
	beforePrevious := make([]int, len(b)+1)
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				current[j] = min(current[j], beforePrevious[j-2]+1)
			}
		}
		beforePrevious, previous, current = previous, current, beforePrevious
	}
	return previous[len(b)]
}
