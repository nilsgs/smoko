package parser

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var tagNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// NormalizeTag validates a tag name and strips an optional leading @.
func NormalizeTag(raw string) (string, error) {
	tag := strings.TrimSpace(raw)
	tag = strings.TrimPrefix(tag, "@")
	if tag == "" {
		return "", fmt.Errorf("tag is empty")
	}
	if !tagNamePattern.MatchString(tag) {
		return "", fmt.Errorf("tag %q must match [A-Za-z0-9][A-Za-z0-9_-]*", raw)
	}
	return tag, nil
}

// MergeTags deduplicates and sorts tag names.
func MergeTags(groups ...[]string) []string {
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, tag := range group {
			if tag != "" {
				seen[tag] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}

	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func parseTagLine(line string) ([]string, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, fmt.Errorf("tag line is empty")
	}

	tags := make([]string, 0, len(fields))
	for _, field := range fields {
		if !strings.HasPrefix(field, "@") {
			return nil, fmt.Errorf("tag %q must start with @", field)
		}
		tag, err := NormalizeTag(field)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return MergeTags(tags), nil
}
