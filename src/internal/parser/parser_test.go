package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/parser"
)

func TestParseBasicFeature(t *testing.T) {
	src := `Feature: Hello World
  Testing hello

  Scenario: Print greeting
    When I run "echo hello"
    Then exit code is 0
    Then output contains "hello"
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	require.Len(t, features, 1)

	f := features[0]
	assert.Equal(t, "Hello World", f.Name)
	assert.Len(t, f.Scenarios, 1)

	sc := f.Scenarios[0]
	assert.Equal(t, "Print greeting", sc.Name)
	assert.Len(t, sc.Steps, 3)

	assert.Equal(t, parser.StepWhen, sc.Steps[0].ResolvedType)
	assert.Equal(t, `I run "echo hello"`, sc.Steps[0].Text)

	assert.Equal(t, parser.StepThen, sc.Steps[1].ResolvedType)
	assert.Equal(t, "exit code is 0", sc.Steps[1].Text)

	assert.Equal(t, parser.StepThen, sc.Steps[2].ResolvedType)
	assert.Equal(t, `output contains "hello"`, sc.Steps[2].Text)
}

func TestParseBackground(t *testing.T) {
	src := `Feature: With Background
  Background:
    Given a file "config.json" with content:
      {"key": "val"}

  Scenario: Uses background
    When I run "cat config.json"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	require.Len(t, features, 1)

	f := features[0]
	require.Len(t, f.Background, 1)
	bg := f.Background[0]
	assert.Equal(t, parser.StepGiven, bg.ResolvedType)
	assert.Equal(t, `a file "config.json" with content:`, bg.Text)
	assert.Equal(t, `{"key": "val"}`, bg.Block)
}

func TestParseAndBut(t *testing.T) {
	src := `Feature: And But Test
  Scenario: Multiple assertions
    Given a file "a.txt" with content:
      hello
    And a file "b.txt" with content:
      world
    When I run "cat a.txt b.txt"
    Then exit code is 0
    And output contains "hello"
    But output does not contain "error"
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	sc := features[0].Scenarios[0]

	// Steps: Given, And(Given), When, Then, And(Then), But(Then)
	assert.Equal(t, parser.StepGiven, sc.Steps[0].ResolvedType)
	assert.Equal(t, parser.StepGiven, sc.Steps[1].ResolvedType) // And -> Given
	assert.Equal(t, parser.StepAnd, sc.Steps[1].Type)
	assert.Equal(t, parser.StepWhen, sc.Steps[2].ResolvedType)
	assert.Equal(t, parser.StepThen, sc.Steps[3].ResolvedType)
	assert.Equal(t, parser.StepThen, sc.Steps[4].ResolvedType) // And -> Then
	assert.Equal(t, parser.StepThen, sc.Steps[5].ResolvedType) // But -> Then
}

func TestParseMultiLineBlock(t *testing.T) {
	src := `Feature: Multiline
  Scenario: File with content
    Given a file "script.sh" with content:
      #!/bin/sh
      echo hello
      exit 0
    When I run "sh script.sh"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	sc := features[0].Scenarios[0]
	step := sc.Steps[0]
	assert.Equal(t, "#!/bin/sh\necho hello\nexit 0", step.Block)
}

func TestParseComments(t *testing.T) {
	src := `Feature: Comments
  # This is a comment
  Scenario: With comment
    # Another comment
    When I run "echo ok"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	assert.Len(t, features[0].Scenarios[0].Steps, 2)
}

func TestParseInlineImage(t *testing.T) {
	src := `Feature: With Image
  Image: myimage:1.0

  Scenario: Basic
    When I run "echo ok"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	assert.Equal(t, "myimage:1.0", features[0].Image)
}

func TestParseMultipleScenarios(t *testing.T) {
	src := `Feature: Multi
  Scenario: First
    When I run "echo first"
    Then exit code is 0
  Scenario: Second
    When I run "echo second"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	assert.Len(t, features[0].Scenarios, 2)
	assert.Equal(t, "First", features[0].Scenarios[0].Name)
	assert.Equal(t, "Second", features[0].Scenarios[1].Name)
}

func TestParseFeatureAndScenarioTags(t *testing.T) {
	src := `@cli @git
Feature: Tagged

  @dirty
  # comments between tags and scenarios are allowed
  @requires-docker
  Scenario: Dirty repo
    When I run "true"
    Then exit code is 0

  @clean
  Scenario: Clean repo
    When I run "true"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	require.Len(t, features, 1)

	assert.Equal(t, []string{"cli", "git"}, features[0].Tags)
	require.Len(t, features[0].Scenarios, 2)
	assert.Equal(t, []string{"dirty", "requires-docker"}, features[0].Scenarios[0].Tags)
	assert.Equal(t, []string{"clean"}, features[0].Scenarios[1].Tags)
}

func TestParseTagsBeforeNextFeature(t *testing.T) {
	src := `Feature: First
  Scenario: One
    When I run "true"
    Then exit code is 0

@second
Feature: Second
  Scenario: Two
    When I run "true"
    Then exit code is 0
`
	features, err := parser.ParseFile("test.smoko", src)
	require.NoError(t, err)
	require.Len(t, features, 2)

	assert.Empty(t, features[0].Tags)
	assert.Equal(t, []string{"second"}, features[1].Tags)
}

func TestParseRejectsInvalidTagSyntax(t *testing.T) {
	src := `@needs.docker
Feature: Invalid
  Scenario: One
    When I run "true"
    Then exit code is 0
`
	_, err := parser.ParseFile("test.smoko", src)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "test.smoko:1")
	assert.Contains(t, err.Error(), "must match")
}

func TestParseRejectsTagsBeforeBackground(t *testing.T) {
	src := `Feature: Invalid
  @setup
  Background:
    Given a file "x" exists

  Scenario: One
    When I run "true"
    Then exit code is 0
`
	_, err := parser.ParseFile("test.smoko", src)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tags may only apply to Feature or Scenario")
}
