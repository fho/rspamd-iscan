package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fho/rspamd-iscan/internal/testutils/assert"
)

func TestLoadCredentialsFromDirectory_NoDirSet(t *testing.T) {
	cfg := &Config{RspamdPassword: "original"}
	err := cfg.LoadCredentialsFromDirectory("")
	assert.NoError(t, err)
	assert.Equal(t, "original", cfg.RspamdPassword)
}

func TestLoadCredentialsFromDirectory(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte("secret123"), 0600)
	os.WriteFile(filepath.Join(dir, "ImapUser"), []byte("testuser"), 0600)

	cfg := &Config{
		RspamdURL:      "http://original.url",
		RspamdPassword: "original",
	}

	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.NoError(t, err)
	assert.Equal(t, "secret123", cfg.RspamdPassword)
	assert.Equal(t, "testuser", cfg.ImapUser)
	assert.Equal(t, "http://original.url", cfg.RspamdURL)
}

func TestLoadCredentialsFromDirectory_MissingFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte("secret"), 0600)

	cfg := &Config{ImapPassword: "original"}

	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.NoError(t, err)
	assert.Equal(t, "secret", cfg.RspamdPassword)
	assert.Equal(t, "original", cfg.ImapPassword)
}

func TestLoadCredentialsFromDirectory_EmptyFileError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte(""), 0600)

	cfg := &Config{}
	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.Error(t, err)
}

func TestLoadCredentialsFromDirectory_PreservesSpaces(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte(" spaces \n"), 0600)
	os.WriteFile(filepath.Join(dir, "ImapPassword"), []byte(" spaces \r\n"), 0600)

	cfg := &Config{}
	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.NoError(t, err)
	assert.Equal(t, " spaces ", cfg.RspamdPassword)
	assert.Equal(t, " spaces ", cfg.ImapPassword)
}
