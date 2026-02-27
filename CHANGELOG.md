# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Lightweight Kubernetes testing framework powered by kube-apiserver and kine.
- Pool-based instance management with configurable size and acquire timeout.
- Release strategies: `ReleaseRestart`, `ReleaseClean`, `ReleasePurge`, `ReleaseNone`.
- CRD pre-loading with directory caching.
- SQLite database prepopulation via `WithPrepopulateDB`.
- Namespace-isolated parallel test execution.
