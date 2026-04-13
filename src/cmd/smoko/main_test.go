package main

import (
	"runtime"
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
