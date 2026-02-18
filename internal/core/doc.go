// Package core provides the internal implementation of the k8senv testing framework.
//
// The primary types are:
//   - [Manager]: state machine with two-phase initialization (NewManagerWithConfig / Initialize),
//     CRD cache creation, and parallel shutdown with drain timeout.
//   - [Pool]: bounded LIFO collection with on-demand instance creation, blocking acquire
//     when exhausted, and double-release detection.
//   - [Instance]: lazy-started kube-apiserver wrapper with atomic state transitions,
//     port-conflict retry, and configurable namespace cleanup on release.
//   - [ManagerConfig] and [InstanceConfig]: validated, immutable configuration structs
//     that control timeouts, pool size, release strategy, and binary paths.
package core
