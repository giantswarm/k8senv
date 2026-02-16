// Package core provides the internal implementation of the k8senv testing framework.
// It contains the Manager (state machine with two-phase initialization and parallel shutdown),
// Pool (bounded LIFO collection with on-demand instance creation and double-release detection),
// and Instance (lazy-started kube-apiserver wrapper with atomic state, port-conflict retry,
// and namespace cleanup on release).
package core
