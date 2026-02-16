// Package kubestack provides a unified kine + kube-apiserver process pair.
// Stack manages the coordinated lifecycle of both processes, starting them in
// parallel via errgroup and shutting them down in reverse order (apiserver first,
// then kine). It delegates port allocation to netutil.PortRegistry to guarantee
// distinct ports across concurrent stacks.
package kubestack
