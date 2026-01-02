package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fho/rspamd-iscan/internal/testutils/assert"
)

func TestLoadCredentialsFromDirectory(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte("secret123"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "ImapUser"), []byte("testuser"), 0600)

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
	_ = os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte("secret"), 0600)

	cfg := &Config{ImapPassword: "original"}

	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.NoError(t, err)
	assert.Equal(t, "secret", cfg.RspamdPassword)
	assert.Equal(t, "original", cfg.ImapPassword)
}

func TestLoadCredentialsFromDirectory_DirNotExistsError(t *testing.T) {
	cfg := &Config{}
	err := cfg.LoadCredentialsFromDirectory("/nonexistent/path")
	assert.Error(t, err)
	assert.Equal(t, "credentials directory: stat /nonexistent/path: no such file or directory", err.Error())
}

func TestLoadCredentialsFromDirectory_EmptyFileError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte(""), 0600)

	cfg := &Config{}
	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.Error(t, err)
	assert.Equal(t, "reading credential RspamdPassword: file is empty", err.Error())
}

func TestLoadCredentialsFromDirectory_PreservesSpaces(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "RspamdPassword"), []byte(" spaces \nnewline\n"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "ImapPassword"), []byte(" spaces \r\nnewline\n\r"), 0600)

	cfg := &Config{}
	err := cfg.LoadCredentialsFromDirectory(dir)
	assert.NoError(t, err)
	assert.Equal(t, " spaces \nnewline", cfg.RspamdPassword)
	assert.Equal(t, " spaces \r\nnewline", cfg.ImapPassword)
}
