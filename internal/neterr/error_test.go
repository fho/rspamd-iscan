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
