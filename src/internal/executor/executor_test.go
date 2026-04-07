package executor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
)

func TestCollectEnvVars(t *testing.T) {
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `environment variable "FOO" is set to "bar"`},
		{ResolvedType: parser.StepGiven, Text: `a file "x.txt" exists`},
		{ResolvedType: parser.StepGiven, Text: `environment variable "BAZ" is set to "qux"`},
		{ResolvedType: parser.StepWhen, Text: `I run "env"`},
	}

	got := executor.CollectEnvVars(steps)
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, got)
}

func TestCollectEnvVarsEmpty(t *testing.T) {
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `a file "x.txt" exists`},
		{ResolvedType: parser.StepWhen, Text: `I run "ls"`},
	}
	got := executor.CollectEnvVars(steps)
	assert.Nil(t, got)
}

func TestCollectEnvVarsIgnoresNonGiven(t *testing.T) {
	steps := []parser.Step{
		// StepThen should not be collected even if text matches pattern
		{ResolvedType: parser.StepThen, Text: `environment variable "FOO" is set to "bar"`},
	}
	got := executor.CollectEnvVars(steps)
	assert.Nil(t, got)
}
