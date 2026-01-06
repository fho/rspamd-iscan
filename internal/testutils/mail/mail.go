package mail

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	SpamMailSubject = "Test spam mail (GTUBE)"
	HamMailSubject  = "An RFC 822 formatted message"
)

func findProjectRoot(t *testing.T) string {
	t.Helper()
	const projectRootfile = "go.mod"
	path, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not detect working dir: %s", err)
	}

	for {
		_, err = os.Stat(filepath.Join(path, projectRootfile))
		if err == nil {
			return path
		}

		if os.IsNotExist(err) {
			subdir := filepath.Join(path, "..")
			if subdir == path {
				t.Fatalf("could not find project root directory containing %q file", projectRootfile)
			}
			path = subdir

			continue
		}
		t.Fatalf("checking if directory exists failed: %s", err)
		return ""
	}
}

func TestHamMailPath(t *testing.T) string {
	proot := findProjectRoot(t)
	return filepath.Join(proot, "internal", "testutils", "mail", "testdata", "example.mail")
}

func TestSpamMailPath(t *testing.T) string {
	proot := findProjectRoot(t)
	return filepath.Join(proot, "internal", "testutils", "mail", "testdata", "spam.mail")
}

func TestMalformedMailPath(t *testing.T) string {
	proot := findProjectRoot(t)
	return filepath.Join(proot, "internal", "testutils", "mail", "testdata", "malformed_envelope.mail")
}
