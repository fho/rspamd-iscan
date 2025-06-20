package mail

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxLineLength is the max. allowed numbers of characters per line an e-mail,
// *including* the terminating CRLF
// (https://datatracker.ietf.org/doc/html/rfc2822#section-3.5)
const maxLineLength = 1000

// strEmailHdrCharsOnly removes all non-printable ASCII chars and colons from s
func strEmailHdrCharsOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 33 && r <= 126 && r != ':' {
			return r
		}

		return -1
	}, s)
}

// AsHeader converts the header name and body to an email header line.
// The line is terminated with \r\n.
// If the header is invalid because it is too long or name or body contain
// an invalid characters an error is returned.
//
// https://datatracker.ietf.org/doc/html/rfc2822#section-2.2
func AsHeader(name, w string) ([]byte, error) {
	nClean := strEmailHdrCharsOnly(name)
	if len(nClean) != len(name) {
		return nil, errors.New("header name contains an invalid character")
	}

	bClean := strEmailHdrCharsOnly(w)
	if len(bClean) != len(w) {
		return nil, errors.New("header body contains an invalid character")
	}

	hdr := append([]byte(nClean), ':', ' ')
	hdr = append(hdr, []byte(bClean)...)
	hdr = append(hdr, '\r', '\n')
	if len(hdr) > maxLineLength {
		return nil, errors.New("header is too long")
	}

	return hdr, nil
}

// AsHeaders converts the map to an email header section
func AsHeaders(hdrs map[string]string) ([]byte, error) {
	result := make([]byte, 0, 4096)

	i := 0
	for k, v := range hdrs {
		hdr, err := AsHeader(k, v)
		if err != nil {
			return nil, fmt.Errorf("converting header '%q: %q' failed: %w", k, v, err)
		}

		result = append(result, hdr...)
		i++
	}

	return slices.Clip(result), nil
}

// AddHeaders inserts additional headers to the e-mail at [path].
// The file must be in RFC2822 format.
func AddHeaders(path string, headers []byte) error {
	tmpfileFd, err := os.CreateTemp("", filepath.Base(path))
	if err != nil {
		return err
	}

	emailFd, err := os.Open(path)
	if err != nil {
		return err
	}
	defer emailFd.Close()

	err = addHeaders(emailFd, tmpfileFd, headers)
	if err != nil {
		_ = tmpfileFd.Close()
		delErr := os.Remove(tmpfileFd.Name())
		return errors.Join(err, delErr)
	}

	if err := tmpfileFd.Close(); err != nil {
		delErr := os.Remove(tmpfileFd.Name())
		return errors.Join(
			fmt.Errorf("writing tempfile failed: %w", err),
			delErr,
		)
	}

	return os.Rename(tmpfileFd.Name(), path)
}

// addHeaders reads an email from in, inserts the additional headers and writes
// the result to out.
func addHeaders(in io.Reader, out io.Writer, hdrs []byte) error {
	emailBr := bufio.NewReader(in)
	tmpfileBw := bufio.NewWriter(out)

	buf := make([]byte, 4096)

	for {
		n, err := emailBr.Read(buf)
		if err != nil {
			if err == io.EOF { //nolint:errorlint // errors.Is unnecessary here
				return errors.New("header end not found")
			}
			return fmt.Errorf("reading email failed: %w", err)
		}

		idx := bytes.Index(buf[:n], []byte("\r\n\r\n"))
		if idx != -1 {
			// header end found, copy up to the delim
			_, err = tmpfileBw.Write(buf[:idx])
			if err != nil {
				return fmt.Errorf("copying data failed: %w", err)
			}

			// +2 to skip the \r\n of the last header line, all
			// lines in hdrs are already expected to be \r\n
			// terminated
			buf = buf[idx+2 : n]
			break
		}

		_, err = tmpfileBw.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("copying data failed: %w", err)
		}
	}

	if _, err := tmpfileBw.Write([]byte("\r\n")); err != nil {
		return fmt.Errorf("writing failed: %w", err)
	}

	if _, err := tmpfileBw.Write(hdrs); err != nil {
		return fmt.Errorf("writing failed: %w", err)
	}

	// write remaining data from already read buffer
	if _, err := tmpfileBw.Write(buf); err != nil {
		return fmt.Errorf("writing failed: %w", err)
	}

	if _, err := io.Copy(tmpfileBw, emailBr); err != nil {
		return fmt.Errorf("copying email failed: %w", err)
	}

	if err := tmpfileBw.Flush(); err != nil {
		return fmt.Errorf("flushing buffer failed: %w", err)
	}

	return nil
}
