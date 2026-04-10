package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/config"
)

func TestLoadAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Image)
	assert.Equal(t, 0, cfg.Timeout)
}

func TestLoadPresent(t *testing.T) {
	dir := t.TempDir()
	content := `image   = "myimage:latest"
timeout = 60
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".smokorc"), []byte(content), 0644))
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "myimage:latest", cfg.Image)
	assert.Equal(t, 60, cfg.Timeout)
}

func TestLoadWithBuild(t *testing.T) {
	dir := t.TempDir()
	content := `image   = "myimage:latest"
timeout = 60
build   = "docker build -t myimage:latest ."
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".smokorc"), []byte(content), 0644))
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "myimage:latest", cfg.Image)
	assert.Equal(t, 60, cfg.Timeout)
	assert.Equal(t, "docker build -t myimage:latest .", cfg.Build)
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".smokorc"), []byte("[[invalid"), 0644))
	_, err := config.Load(dir)
	assert.Error(t, err)
}
