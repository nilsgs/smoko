package hints_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nskut/smoko/internal/hints"
)

var thenPatterns = []string{
	`exit code is 0`,
	`output contains "text"`,
	`output does not contain "text"`,
	`stdout contains "text"`,
	`stderr contains "text"`,
	`output matches pattern "regex"`,
	`stdout matches pattern "regex"`,
	`stderr matches pattern "regex"`,
	`file "path" exists`,
	`file "path" does not exist`,
	`file "path" contains "text"`,
}

func TestSuggestTypo(t *testing.T) {
	// "match" instead of "matches"
	got := hints.Suggest(`stderr match pattern "error"`, thenPatterns)
	assert.Equal(t, `stderr matches pattern "regex"`, got)
}

func TestSuggestSingleWordTypo(t *testing.T) {
	// "contain" instead of "contains"
	got := hints.Suggest(`output contain "hello"`, thenPatterns)
	assert.Equal(t, `output contains "text"`, got)
}

func TestSuggestNoMatch(t *testing.T) {
	// completely unrelated text should return no suggestion
	got := hints.Suggest("xyzzy florp blarg", thenPatterns)
	assert.Equal(t, "", got)
}

func TestSuggestExactMatch(t *testing.T) {
	got := hints.Suggest(`output contains "hello"`, thenPatterns)
	assert.Equal(t, `output contains "text"`, got)
}

func TestSuggestGivenTypo(t *testing.T) {
	given := []string{
		`a file "path" exists`,
		`the directory "path" exists`,
		`I run "command"`,
	}
	got := hints.Suggest(`a file "foo.txt" exist`, given)
	assert.Equal(t, `a file "path" exists`, got)
}
