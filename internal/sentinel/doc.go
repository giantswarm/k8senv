// Package sentinel provides an immutable error type for sentinel error declarations.
//
// Sentinel errors declared with errors.New are mutable variables that consumers
// can reassign. This package provides Error, a string-based error type that can
// be declared as a const, making sentinel errors truly immutable while remaining
// compatible with errors.Is for wrapped error chain comparison.
package sentinel
