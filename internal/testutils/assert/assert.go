package assert

import (
	"strings"
	"testing"
)

func NoError(t *testing.T, err error, msg ...string) {
	t.Helper()

	if err != nil {
		if len(msg) == 0 {
			t.Fatal(err)
		}

		t.Fatal(strings.Join(msg, " "), ", error: ", err.Error())
	}
}

func Error(t *testing.T, err error, msg ...string) {
	t.Helper()

	if err == nil {
		if len(msg) == 0 {
			t.Fatal(err)
		}

		t.Fatal(strings.Join(msg, " "), ", expected an error but got nil")
	}
}

func Equal[T comparable](t *testing.T, expected, actual T) {
	t.Helper()
	if expected != actual {
		t.Fatalf("Not equal, expecting '%v', got: '%v'", expected, actual)
	}
}

func NotEqual[T comparable](t *testing.T, expected, actual T) {
	t.Helper()
	if expected == actual {
		t.Fatalf("expecting not equal values, got: '%v'", expected)
	}
}
