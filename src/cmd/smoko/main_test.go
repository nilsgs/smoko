package main

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/config"
	"github.com/nskut/smoko/internal/reporter"
)

func TestRunCmdDefaults(t *testing.T) {
	cmd := runCmd()

	assert.Equal(t, "1", cmd.Flags().Lookup("timeout").DefValue)
	assert.Equal(t, "0", cmd.Flags().Lookup("parallel").DefValue)
	assert.Equal(t, "", cmd.Flags().Lookup("output").DefValue)
}

func TestResolveTimeoutUsesBuiltInDefault(t *testing.T) {
	timeout := resolveTimeout(config.Config{}, config.DefaultTimeout, false)

	assert.Equal(t, time.Second, timeout)
}

func TestResolveTimeoutUsesConfigWhenFlagNotSet(t *testing.T) {
	timeout := resolveTimeout(config.Config{Timeout: 5}, config.DefaultTimeout, false)

	assert.Equal(t, 5*time.Second, timeout)
}

func TestResolveTimeoutUsesFlagWhenSet(t *testing.T) {
	timeout := resolveTimeout(config.Config{Timeout: 5}, 2, true)

	assert.Equal(t, 2*time.Second, timeout)
}

func TestResolveWorkerCountUsesAutoForZeroOrLess(t *testing.T) {
	workers := resolveWorkerCount(0)

	assert.Equal(t, runtime.GOMAXPROCS(0), workers)
}

func TestResolveWorkerCountUsesExplicitValue(t *testing.T) {
	workers := resolveWorkerCount(3)

	assert.Equal(t, 3, workers)
}

func TestParseOutputModeDefaultsToText(t *testing.T) {
	mode, err := parseOutputMode("")

	require.NoError(t, err)
	assert.Equal(t, reporter.OutputModeText, mode)
}

func TestParseOutputModeAcceptsJSON(t *testing.T) {
	mode, err := parseOutputMode("json")

	require.NoError(t, err)
	assert.Equal(t, reporter.OutputModeJSON, mode)
}

func TestParseOutputModeRejectsUnknownValue(t *testing.T) {
	_, err := parseOutputMode("human")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "supported: json")
}

func echoCmd(msg string) string {
	if runtime.GOOS == "windows" {
		return "echo " + msg
	}
	return "echo " + msg
}

func failCmd() string {
	if runtime.GOOS == "windows" {
		return "exit 1"
	}
	return "exit 1"
}

func TestRunBuildSuppressesOutputOnSuccess(t *testing.T) {
	// Redirect stderr to capture the "Building:" header and any build output.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w

	buildErr := runBuild(echoCmd("build-output-should-be-hidden"), t.TempDir(), reporter.OutputModeText, false)

	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	require.NoError(t, buildErr)
	// Strip the "Building: <cmd>" header line, then verify no command output leaked through.
	var nonHeader []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "Building:") {
			nonHeader = append(nonHeader, line)
		}
	}
	remaining := strings.Join(nonHeader, "\n")
	assert.False(t, strings.Contains(remaining, "build-output-should-be-hidden"), "build stdout should be suppressed on success")
}

func TestRunBuildPrintsOutputOnFailure(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo build-failure-output && exit 1"
	} else {
		cmd = "echo build-failure-output && exit 1"
	}
	buildErr := runBuild(cmd, t.TempDir(), reporter.OutputModeText, false)

	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	require.Error(t, buildErr)
	assert.Contains(t, buildErr.Error(), "build failed")
	assert.Contains(t, out, "build-failure-output", "captured build output should be printed on failure")
}

func TestRunBuildVerboseStreamsOutput(t *testing.T) {
	// In verbose mode runBuild streams directly — it should not return an error on success.
	err := runBuild(echoCmd("verbose-output"), t.TempDir(), reporter.OutputModeText, true)
	require.NoError(t, err)
}
