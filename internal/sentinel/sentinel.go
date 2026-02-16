package sentinel

// Compile-time check that Error implements the error interface.
var _ error = Error("")

// Error is an immutable error type backed by a string constant.
// Unlike errors.New, which returns a pointer and must be stored in a var,
// Error values can be declared as const, preventing reassignment.
//
// errors.Is compatibility: since Error is a comparable type, the default
// == comparison used by errors.Is works correctly through wrapped error chains.
type Error string

// Error implements the error interface.
func (e Error) Error() string {
	return string(e)
}
