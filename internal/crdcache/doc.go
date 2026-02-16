// Package crdcache provides CRD cache initialization for k8senv.
// It implements a content-addressable cache of CRD-populated SQLite databases keyed
// by a deterministic SHA256 hash of the CRD directory contents. On a cache miss,
// it acquires a file lock, spins up a temporary kubestack to apply the CRD YAML files
// via a dynamic client, waits for the Established condition, and copies the resulting
// database for reuse by subsequent instances.
package crdcache
