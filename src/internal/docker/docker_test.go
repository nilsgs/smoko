package docker

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBatchFSCheckOutputPreservesFileContent(t *testing.T) {
	checks := []FSCheck{
		{Kind: FSCheckReadFile, Path: "first.txt"},
		{Kind: FSCheckReadFile, Path: "second.txt"},
	}
	firstContent := "alpha\n__SMOKO_CHK_1_END__\nomega\n\n"
	secondContent := "second\n"
	stdout := fmt.Sprintf(
		"__SMOKO_CHK_0_START__\nOK %d\n%s\n__SMOKO_CHK_0_END__\n__SMOKO_CHK_1_START__\nOK %d\n%s\n__SMOKO_CHK_1_END__\n",
		len(firstContent),
		firstContent,
		len(secondContent),
		secondContent,
	)

	results := parseBatchFSCheckOutput(stdout, checks)

	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.NoError(t, results[1].Err)
	assert.Equal(t, firstContent, results[0].Content)
	assert.Equal(t, secondContent, results[1].Content)
}

func TestParseBatchFSCheckOutputHandlesExistsChecks(t *testing.T) {
	checks := []FSCheck{
		{Kind: FSCheckFileExists, Path: "present.txt"},
		{Kind: FSCheckDirExists, Path: "missing"},
	}
	stdout := "__SMOKO_CHK_0_START__\nYES\n__SMOKO_CHK_0_END__\n__SMOKO_CHK_1_START__\nNO\n__SMOKO_CHK_1_END__\n"

	results := parseBatchFSCheckOutput(stdout, checks)

	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.NoError(t, results[1].Err)
	assert.True(t, results[0].Exists)
	assert.False(t, results[1].Exists)
}

func TestParseBatchFSCheckOutputReportsMissingFile(t *testing.T) {
	checks := []FSCheck{{Kind: FSCheckReadFile, Path: "missing.txt"}}
	stdout := "__SMOKO_CHK_0_START__\nERR\n__SMOKO_CHK_0_END__\n"

	results := parseBatchFSCheckOutput(stdout, checks)

	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
	assert.Contains(t, results[0].Err.Error(), "missing.txt")
}

func TestWorkPathResolvesUnderWorkDir(t *testing.T) {
	got, err := WorkPath("project/file.txt")

	require.NoError(t, err)
	assert.Equal(t, "/smoko-work/project/file.txt", got)
}

func TestWorkPathAllowsAbsoluteWorkDirPath(t *testing.T) {
	got, err := WorkPath("/smoko-work/project")

	require.NoError(t, err)
	assert.Equal(t, "/smoko-work/project", got)
}

func TestWorkPathRejectsTraversal(t *testing.T) {
	_, err := WorkPath("../outside.txt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}

func TestWorkPathRejectsAbsoluteOutsideWorkDir(t *testing.T) {
	_, err := WorkPath("/tmp/out.txt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "/smoko-work")
}

func TestAssertionPathAllowsAbsoluteOutsideWorkDir(t *testing.T) {
	got, err := AssertionPath("/tmp/out.txt")

	require.NoError(t, err)
	assert.Equal(t, "/tmp/out.txt", got)
}

func TestAssertionPathResolvesRelativeUnderWorkDir(t *testing.T) {
	got, err := AssertionPath("out.txt")

	require.NoError(t, err)
	assert.Equal(t, "/smoko-work/out.txt", got)
}

func TestAssertionPathRejectsTraversal(t *testing.T) {
	_, err := AssertionPath("project/../secret.txt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}
