package mail

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func AssertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func AssertErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error but got none")
	}
}

func TestAddHeaders(t *testing.T) {
	const expected = "From: someone@example.com\r\nTo: someone_else@example.com\r\nSubject: An RFC 822 formatted message\r\nNew-Header1: v1\r\nNew-Header2: v2\r\n\r\nThis is the plain text body of the message. Note the blank line\r\nbetween the header information and the body of the message.\r\n"

	tmpdir := t.TempDir()
	fd, err := os.CreateTemp(tmpdir, t.Name())
	AssertNoErr(t, err)

	exampleFd, err := os.Open(filepath.Join("testdata", "example.mail"))
	AssertNoErr(t, err)

	_, err = io.Copy(fd, exampleFd)
	AssertNoErr(t, err)

	err = fd.Close()
	AssertNoErr(t, err)
	_ = exampleFd.Close()

	hdrs, err := AsHeaders(map[string]string{
		"New-Header1": "v1",
		"New-Header2": "v2",
	})
	AssertNoErr(t, err)
	err = AddHeaders(fd.Name(), hdrs)
	AssertNoErr(t, err)

	result, err := os.ReadFile(fd.Name())
	AssertNoErr(t, err)

	if !bytes.Equal(result, []byte(expected)) {
		t.Errorf("Got:\n%q\nExpected:\n%q\n", string(result), expected)
	}
}
