package imapclt

import "fmt"

type ErrMalformedMsg struct {
	Err string
	UID uint32
}

func NewErrMalformedMsg(err string, uid uint32) *ErrMalformedMsg {
	return &ErrMalformedMsg{
		Err: err,
		UID: uid,
	}
}

func (e *ErrMalformedMsg) Error() string {
	return fmt.Sprintf("mail UID: %d: %s", e.UID, e.Err)
}
