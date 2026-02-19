package sentinel

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_Error(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  Error
		want string
	}{
		"simple message": {err: Error("something failed"), want: "something failed"},
		"empty message":  {err: Error(""), want: ""},
		"with space":     {err: Error("not found"), want: "not found"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestError_ImplementsErrorInterface(t *testing.T) {
	t.Parallel()

	var err error = Error("test")
	if err.Error() != "test" {
		t.Errorf("expected 'test', got %q", err.Error())
	}
}

func TestError_ErrorsIs(t *testing.T) {
	t.Parallel()

	const sentinel = Error("not found")

	t.Run("direct match", func(t *testing.T) {
		t.Parallel()

		if !errors.Is(sentinel, sentinel) {
			t.Error("errors.Is should match identical sentinel errors")
		}
	})

	t.Run("wrapped match", func(t *testing.T) {
		t.Parallel()

		wrapped := fmt.Errorf("operation failed: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Error("errors.Is should match sentinel error through wrapping")
		}
	})

	t.Run("different sentinel no match", func(t *testing.T) {
		t.Parallel()

		const other = Error("other error")
		if errors.Is(sentinel, other) {
			t.Error("errors.Is should not match different sentinel errors")
		}
	})

	t.Run("same text different type no match", func(t *testing.T) {
		t.Parallel()

		stdErr := errors.New("not found")
		if errors.Is(sentinel, stdErr) {
			t.Error("errors.Is should not match sentinel error against errors.New with same text")
		}
	})
}

func TestError_CanDeclareAsConst(t *testing.T) {
	t.Parallel()

	// This test verifies at compile time that Error can be used as a const.
	const errConst = Error("constant error")
	if errConst.Error() != "constant error" {
		t.Error("const Error should return its string value")
	}
}
