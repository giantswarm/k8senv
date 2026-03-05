# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Removed

- **BREAKING**: Removed `ErrNotStarted` from public API.
- **BREAKING**: Removed `ErrPoolClosed` from public API.
- **BREAKING**: Removed CRD sentinel errors from public API: `ErrNoYAMLFiles`, `ErrMissingKind`, `ErrCRDEstablishTimeout`.
- **BREAKING**: Removed `SystemNamespaceNames()` function.
- **BREAKING**: Unexported `DefaultBaseDataDirName` constant.

### Changed

- Bumped kube-apiserver dependency to v1.35.2.
- `ConfigSnapshot` is now a type alias for the internal config, eliminating field-by-field copying.

### Fixed

- Fixed panic in `configDiffs` when comparing uncomparable option fields.
- Fixed double-close and destructive non-atomic cleanup in file copy utilities.
- Reduced TOCTOU window in port allocation by reserving ports before closing listeners.

### Refactored

- ~55 internal refactoring commits across `crdcache`, `fileutil`, `netutil`, `process`, `kubestack`, and `core` packages — no public API changes beyond the removals above.
- CI now runs integration tests with kine and kube-apiserver.

## [0.2.0] - 2026-03-03

### Removed

- **BREAKING**: Removed `ReleaseStrategy` type and all variants (`ReleaseRestart`, `ReleaseClean`, `ReleasePurge`, `ReleaseNone`).
- **BREAKING**: Removed `WithReleaseStrategy()` manager option.
- Removed API-based namespace cleanup (`internal/core/cleanup.go`).
- Removed test packages: `tests/cleanup/`, `tests/restart/`, `tests/stressclean/`, `tests/stresspurge/`.

### Changed

- `Release()` now always purges non-system namespaces via direct SQLite queries (previously the `ReleasePurge` strategy). This is the fastest and most reliable cleanup method.
- Simplified internal instance lifecycle — no more strategy-based branching.

## [0.1.0] - 2026-03-02

## [0.0.4] - 2026-03-02

### Added

- Lightweight Kubernetes testing framework powered by kube-apiserver and kine.
- Pool-based instance management with configurable size and acquire timeout.
- Release strategies: `ReleaseRestart`, `ReleaseClean`, `ReleasePurge`, `ReleaseNone`.
- CRD pre-loading with directory caching.
- SQLite database prepopulation via `WithPrepopulateDB`.
- Namespace-isolated parallel test execution.

[Unreleased]: https://github.com/giantswarm/k8senv/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/giantswarm/k8senv/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/giantswarm/k8senv/compare/v0.0.4...v0.1.0
[0.0.4]: https://github.com/giantswarm/k8senv/releases/tag/v0.0.4
