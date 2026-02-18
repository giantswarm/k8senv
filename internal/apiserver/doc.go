// Package apiserver provides process management for the kube-apiserver.
//
// On startup it generates self-signed TLS certificates, a static bearer token
// for authentication, and an AuthenticationConfiguration for anonymous health
// endpoints. Readiness is determined by polling the /livez HTTPS endpoint.
// After the server is ready, WriteKubeconfig produces a kubeconfig file that
// clients can use to connect.
package apiserver
