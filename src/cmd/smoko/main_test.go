package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nskut/smoko/internal/config"
)

func TestRunCmdDefaults(t *testing.T) {
	cmd := runCmd()

	assert.Equal(t, "1", cmd.Flags().Lookup("timeout").DefValue)
	assert.Equal(t, "0", cmd.Flags().Lookup("parallel").DefValue)
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
