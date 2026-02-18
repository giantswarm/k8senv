// Package kine provides process management for the kine etcd-compatible SQLite shim.
//
// It handles the full kine lifecycle: construction, startup with optional SQLite
// database prepopulation from a cached template, TCP-based readiness polling on
// the listen port, and graceful shutdown via SIGTERM with a configurable timeout.
package kine
