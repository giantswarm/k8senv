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

	stressCanaryNS  = "stress-canary"
	stressCanaryCM  = "canary-cm"
	stressCanaryPod = "canary-pod"
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
	// IsNotFound is retryable because in tests the API server's admission
	// controller namespace cache may not have caught up yet after a namespace
	// is created, causing namespaced creates to transiently return NotFound.
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
		t.Fatalf("failed to create namespace %s: %v", name, err)
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

	resType := rng.IntN(StressResTypes)

	switch resType {
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
	default:
		t.Fatalf("unhandled resource type %d; update switch to match StressResTypes=%d", resType, StressResTypes)
	}
}

// named is satisfied by all Kubernetes resource types via embedded ObjectMeta.
// It lets stressCreateWithRetry verify the created resource's name without
// callers having to extract and return it manually.
type named interface {
	GetName() string
}

// stressCreateWithRetry generates a resource name from namePrefix and idx,
// then retries the create call until it succeeds or all retries are exhausted.
// The create function receives the generated name and must return the created
// object (any Kubernetes resource satisfies named via ObjectMeta). The helper
// verifies the returned name matches, using resourceType for error context.
func stressCreateWithRetry[T named](
	t *testing.T,
	resourceType string,
	ns string,
	namePrefix string,
	idx int,
	create func(name string) (T, error),
) {
	t.Helper()

	name := fmt.Sprintf("%s-%d", namePrefix, idx)

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		created, createErr := create(name)
		if createErr != nil {
			return createErr
		}
		if created.GetName() != name {
			// Return error instead of t.Fatalf: Fatalf calls runtime.Goexit,
			// preventing retry.OnError from observing the result.
			return fmt.Errorf("%s name mismatch: want %s, got %s", resourceType, name, created.GetName())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to create %s %s/%s: %v", resourceType, ns, name, err)
	}
}

// StressCreateConfigMap creates a ConfigMap with retry on transient errors.
func StressCreateConfigMap(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	data := map[string]string{"key": fmt.Sprintf("value-%d", idx)}

	stressCreateWithRetry(t, "ConfigMap", ns, "stress-cm", idx, func(name string) (*v1.ConfigMap, error) {
		return client.CoreV1().ConfigMaps(ns).Create(ctx, &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Data:       data,
		}, metav1.CreateOptions{})
	})
}

// StressCreateSecret creates a Secret with retry on transient errors.
func StressCreateSecret(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	stressCreateWithRetry(t, "Secret", ns, "stress-secret", idx, func(name string) (*v1.Secret, error) {
		return client.CoreV1().Secrets(ns).Create(ctx, &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			StringData: map[string]string{"secret": fmt.Sprintf("val-%d", idx)},
		}, metav1.CreateOptions{})
	})
}

// StressCreateService creates a headless Service with retry on transient errors.
func StressCreateService(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	stressCreateWithRetry(t, "Service", ns, "stress-svc", idx, func(name string) (*v1.Service, error) {
		return client.CoreV1().Services(ns).Create(ctx, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Port: int32(
							8080 + idx%1000,
						),
						Protocol: v1.ProtocolTCP,
					},
				},
				ClusterIP: "None",
			},
		}, metav1.CreateOptions{})
	})
}

// StressCreatePod creates a Pod with retry on transient errors.
func StressCreatePod(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	stressCreateWithRetry(t, "Pod", ns, "stress-pod", idx, func(name string) (*v1.Pod, error) {
		return client.CoreV1().Pods(ns).Create(ctx, &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "nginx",
						Image: "nginx",
					},
				},
			},
		}, metav1.CreateOptions{})
	})
}

// StressCreateServiceAccount creates a ServiceAccount with retry on transient errors.
func StressCreateServiceAccount(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	stressCreateWithRetry(t, "ServiceAccount", ns, "stress-sa", idx, func(name string) (*v1.ServiceAccount, error) {
		return client.CoreV1().ServiceAccounts(ns).Create(ctx, &v1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		}, metav1.CreateOptions{})
	})
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

// StressVerifyNoCanary asserts that the canary namespace and its resources do
// not exist, confirming cleanup removed them.
func StressVerifyNoCanary(ctx context.Context, t *testing.T, client kubernetes.Interface) {
	t.Helper()

	_, err := client.CoreV1().Namespaces().Get(ctx, stressCanaryNS, metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Fatalf("canary namespace %q still exists (err=%v)", stressCanaryNS, err)
	}

	_, err = client.CoreV1().ConfigMaps(stressCanaryNS).Get(ctx, stressCanaryCM, metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Fatalf("canary ConfigMap %s/%s still exists (err=%v)", stressCanaryNS, stressCanaryCM, err)
	}

	_, err = client.CoreV1().Pods(stressCanaryNS).Get(ctx, stressCanaryPod, metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Fatalf("canary Pod %s/%s still exists (err=%v)", stressCanaryNS, stressCanaryPod, err)
	}
}

// StressCreateCanary creates a static canary namespace with a ConfigMap and Pod.
func StressCreateCanary(ctx context.Context, t *testing.T, client kubernetes.Interface) {
	t.Helper()

	StressCreateNamespace(ctx, t, client, stressCanaryNS)

	err := retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		_, createErr := client.CoreV1().ConfigMaps(stressCanaryNS).Create(ctx, &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: stressCanaryCM, Namespace: stressCanaryNS},
			Data:       map[string]string{"canary": "true"},
		}, metav1.CreateOptions{})
		return createErr
	})
	if err != nil {
		t.Fatalf("failed to create canary ConfigMap %s/%s: %v", stressCanaryNS, stressCanaryCM, err)
	}

	err = retry.OnError(retry.DefaultBackoff, isRetryable, func() error {
		_, createErr := client.CoreV1().Pods(stressCanaryNS).Create(ctx, &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stressCanaryPod, Namespace: stressCanaryNS},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "canary", Image: "canary"},
				},
			},
		}, metav1.CreateOptions{})
		return createErr
	})
	if err != nil {
		t.Fatalf("failed to create canary Pod %s/%s: %v", stressCanaryNS, stressCanaryPod, err)
	}
}

// StressVerifyCanaryExists asserts that the canary namespace, ConfigMap, and
// Pod all exist, confirming creation succeeded before the instance is released.
func StressVerifyCanaryExists(ctx context.Context, t *testing.T, client kubernetes.Interface) {
	t.Helper()

	if _, err := client.CoreV1().Namespaces().Get(ctx, stressCanaryNS, metav1.GetOptions{}); err != nil {
		t.Fatalf("canary namespace %q not found: %v", stressCanaryNS, err)
	}

	if _, err := client.CoreV1().ConfigMaps(stressCanaryNS).Get(ctx, stressCanaryCM, metav1.GetOptions{}); err != nil {
		t.Fatalf("canary ConfigMap %s/%s not found: %v", stressCanaryNS, stressCanaryCM, err)
	}

	if _, err := client.CoreV1().Pods(stressCanaryNS).Get(ctx, stressCanaryPod, metav1.GetOptions{}); err != nil {
		t.Fatalf("canary Pod %s/%s not found: %v", stressCanaryNS, stressCanaryPod, err)
	}
}

// StressWorker is the common body for stress test workers. It acquires an
// instance, verifies it is clean (only system namespaces), creates random
// namespaces and resources, deletes the namespaces, and releases.
func StressWorker(ctx context.Context, t *testing.T, mgr k8senv.Manager, workerID int, nsPrefix string) {
	t.Helper()

	rng := rand.New(rand.NewPCG(uint64(workerID), 0)) //nolint:gosec // deterministic PRNG for reproducibility

	inst, client := AcquireWithClient(ctx, t, mgr)
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	StressVerifyCleanInstance(ctx, t, client)
	StressVerifyNoCanary(ctx, t, client)

	nsCount := rng.IntN(StressMaxNS) + 1

	for n := range nsCount {
		nsName := UniqueName(nsPrefix)
		StressCreateNamespace(ctx, t, client, nsName)

		resCount := rng.IntN(StressMaxRes) + 1
		for r := range resCount {
			idx := n*StressMaxRes + r
			StressCreateRandomResource(ctx, t, client, nsName, idx, rng)
		}
	}

	StressCreateCanary(ctx, t, client)
	StressVerifyCanaryExists(ctx, t, client)
}
