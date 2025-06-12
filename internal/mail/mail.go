package mail

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxLineLength is the max. allowed numbers of characters per line an e-mail,
// *including* the terminating CRLF
// (https://datatracker.ietf.org/doc/html/rfc2822#section-3.5)
const maxLineLength = 1000

// strEmailHdrCharsOnly removes all non-printable ASCII chars and colons from s
func strEmailHdrCharsOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 33 || r > 126 || r == ':' {
			return -1
		}
		return r
	}, s)
}

// AsHeaders converts m to [RFC2822] E-Mail Headers (Key: Value\r\n)
// Non-allowed characters are dropped, if a key-value has more than
// [maxLineLength]-3 chars, the map entry is omitted.
//
// [RFC2822]: https://datatracker.ietf.org/doc/html/rfc2822#section-2.2
func AsHeaders(m map[string]string) []byte {
	var buf bytes.Buffer

	for k, v := range m {
		hdr := append([]byte(strEmailHdrCharsOnly(k)), ':', ' ')
		hdr = append(hdr, ([]byte(strEmailHdrCharsOnly(v)))...)
		hdr = append(hdr, '\r', '\n')
		if len(hdr) > maxLineLength {
			continue
		}

		buf.WriteString(strEmailHdrCharsOnly(k))
		buf.Write([]byte(": "))
		buf.WriteString(strEmailHdrCharsOnly(v))
		buf.Write([]byte(": "))
		buf.Write([]byte("\r\n"))
	}

	return buf.Bytes()
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

	err = withHeaders(emailFd, tmpfileFd, headers)
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

func withHeaders(in io.Reader, out io.Writer, hdrs []byte) error {
	emailBr := bufio.NewReader(in)
	tmpfileBw := bufio.NewWriter(out)

	buf := make([]byte, 4096)

	for {
		n, err := emailBr.Read(buf)
		if err != nil {
			if err == io.EOF {
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

			buf = buf[idx:n]
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
		return fmt.Errorf("flushing out buffer failed: %w", err)
	}

	return nil
}
