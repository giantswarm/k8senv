//go:build integration

package testutil

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/giantswarm/k8senv"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	// StressMaxNS is the maximum number of namespaces created per stress subtest.
	StressMaxNS = 3

	// StressMaxRes is the maximum number of resources created per namespace.
	StressMaxRes = 5

	// StressResTypes is the number of distinct resource types that can be created.
	StressResTypes = 5

	// defaultStressSubtests is the default number of stress subtests to run.
	defaultStressSubtests = 100
)

var (
	stressSubtestsOnce  sync.Once
	stressSubtestsCount int
)

// StressSubtestCount returns the number of stress subtests to run, reading
// K8SENV_STRESS_SUBTESTS on first call. Panics if the env var is set but invalid.
func StressSubtestCount() int {
	stressSubtestsOnce.Do(func() {
		stressSubtestsCount = defaultStressSubtests
		if v := os.Getenv("K8SENV_STRESS_SUBTESTS"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				panic(fmt.Sprintf("invalid K8SENV_STRESS_SUBTESTS=%q: must be a positive integer", v))
			}

			stressSubtestsCount = n
		}
	})

	return stressSubtestsCount
}

// isRetryable returns true for transient API server errors that may resolve on
// retry (timeouts, throttling, internal errors). Used as the predicate for
// retry.OnError in stress helpers.
func isRetryable(err error) bool {
	return errors.IsNotFound(err) ||
		errors.IsTimeout(err) ||
		errors.IsServerTimeout(err) ||
		errors.IsTooManyRequests(err) ||
		errors.IsInternalError(err) ||
		errors.IsServiceUnavailable(err)
}

// StressCreateNamespace creates a namespace and fails the test on error.
func StressCreateNamespace(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) {
	t.Helper()

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		if created.Name != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit, preventing retry.OnError from observing the result.
			return fmt.Errorf("namespace name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create namespace %s: %v", name, err)
	}
}

// StressDeleteNamespace deletes a namespace, logging a warning on error.
func StressDeleteNamespace(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) {
	t.Helper()

	if err := client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		t.Logf("Warning: failed to delete namespace %s: %v", name, err)
	}
}

// StressCreateRandomResource creates a random Kubernetes resource in the given namespace.
func StressCreateRandomResource(
	ctx context.Context,
	t *testing.T,
	client kubernetes.Interface,
	ns string,
	idx int,
	rng *rand.Rand,
) {
	t.Helper()

	switch rng.IntN(StressResTypes) {
	case 0:
		StressCreateConfigMap(ctx, t, client, ns, idx)
	case 1:
		StressCreateSecret(ctx, t, client, ns, idx)
	case 2:
		StressCreateService(ctx, t, client, ns, idx)
	case 3:
		StressCreatePod(ctx, t, client, ns, idx)
	case 4:
		StressCreateServiceAccount(ctx, t, client, ns, idx)
	}
}

// StressCreateConfigMap creates a ConfigMap with retry on transient errors.
//
//nolint:dupl // Each resource-creation helper builds a distinct Kubernetes object; structural similarity is inherent.
func StressCreateConfigMap(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-cm-%d", idx)
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       map[string]string{"key": fmt.Sprintf("value-%d", idx)},
	}

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		if created.Name != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit, preventing retry.OnError from observing the result.
			return fmt.Errorf("configmap name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create ConfigMap %s/%s: %v", ns, name, err)
	}
}

// StressCreateSecret creates a Secret with retry on transient errors.
//
//nolint:dupl // Each resource-creation helper builds a distinct Kubernetes object; structural similarity is inherent.
func StressCreateSecret(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-secret-%d", idx)
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		StringData: map[string]string{"secret": fmt.Sprintf("val-%d", idx)},
	}

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		if created.Name != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit, preventing retry.OnError from observing the result.
			return fmt.Errorf("secret name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create Secret %s/%s: %v", ns, name, err)
	}
}

// StressCreateService creates a headless Service with retry on transient errors.
func StressCreateService(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-svc-%d", idx)
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:     int32(8080 + idx%1000), //nolint:gosec // idx is bounded by StressMaxNS*StressMaxRes (15)
					Protocol: v1.ProtocolTCP,
				},
			},
			ClusterIP: "None",
		},
	}

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		if created.Name != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit, preventing retry.OnError from observing the result.
			return fmt.Errorf("service name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create Service %s/%s: %v", ns, name, err)
	}
}

// StressCreatePod creates a Pod with retry on transient errors.
func StressCreatePod(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-pod-%d", idx)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "nginx",
					Image: "nginx",
				},
			},
		},
	}

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := client.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		if created.Name != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit, preventing retry.OnError from observing the result.
			return fmt.Errorf("pod name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create Pod %s/%s: %v", ns, name, err)
	}
}

// StressCreateServiceAccount creates a ServiceAccount with retry on transient errors.
func StressCreateServiceAccount(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-sa-%d", idx)
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := client.CoreV1().ServiceAccounts(ns).Create(ctx, sa, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		if created.Name != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit, preventing retry.OnError from observing the result.
			return fmt.Errorf("serviceaccount name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount %s/%s: %v", ns, name, err)
	}
}

// StressVerifyCleanInstance asserts that the instance has only system
// namespaces, confirming the previous release removed all user namespaces.
func StressVerifyCleanInstance(ctx context.Context, t *testing.T, client kubernetes.Interface) {
	t.Helper()

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	sysNS := SystemNamespaces()
	for i := range nsList.Items {
		if _, ok := sysNS[nsList.Items[i].Name]; !ok {
			t.Fatalf("instance not clean on acquire: unexpected namespace %q", nsList.Items[i].Name)
		}
	}
}

// StressWorker is the common body for stress test workers. It acquires an
// instance, verifies it is clean (only system namespaces), creates random
// namespaces and resources, deletes the namespaces, and releases.
func StressWorker(ctx context.Context, t *testing.T, mgr k8senv.Manager, workerID int, nsPrefix string) {
	t.Helper()

	rng := rand.New(rand.NewPCG(uint64(workerID), 0)) //nolint:gosec // deterministic PRNG for reproducibility

	inst, client := AcquireWithClient(ctx, t, mgr)
	defer inst.Release() //nolint:errcheck // safe to ignore in defer; on failure instance is removed from pool

	StressVerifyCleanInstance(ctx, t, client)

	nsCount := rng.IntN(StressMaxNS) + 1
	namespaces := make([]string, 0, nsCount)

	for n := range nsCount {
		nsName := UniqueName(nsPrefix)
		StressCreateNamespace(ctx, t, client, nsName)
		namespaces = append(namespaces, nsName)

		resCount := rng.IntN(StressMaxRes) + 1
		for r := range resCount {
			idx := n*StressMaxRes + r
			StressCreateRandomResource(ctx, t, client, nsName, idx, rng)
		}
	}

	for _, ns := range namespaces {
		StressDeleteNamespace(ctx, t, client, ns)
	}
}
