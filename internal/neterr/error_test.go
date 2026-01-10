package neterr

import (
	"net"
	"testing"

	"github.com/fho/rspamd-iscan/internal/testutils/assert"
)

func TestIsRetryableError_ConnectionRefused(t *testing.T) {
	_, err := net.Dial("tcp", "localhost:59123") // port where nothing is listening
	t.Logf("error: %v", err)
	assert.Error(t, err)
	assert.Equal(t, true, IsRetryableError(err))
}

func TestIsRetryableError_ClosedConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	assert.NoError(t, err)
	defer ln.Close()

	conn, err := net.Dial("tcp", ln.Addr().String())
	assert.NoError(t, err)

	conn.Close()

	_, err = conn.Write([]byte("test"))
	t.Logf("error: %v", err)
	assert.Error(t, err)
	assert.Equal(t, true, IsRetryableError(err))
}
