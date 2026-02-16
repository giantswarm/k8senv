//go:build integration

package k8senv_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

var stressSubtests = 100 // override with K8SENV_STRESS_SUBTESTS env var

func init() {
	if v := os.Getenv("K8SENV_STRESS_SUBTESTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			panic(fmt.Sprintf("invalid K8SENV_STRESS_SUBTESTS=%q: must be a positive integer", v))
		}

		stressSubtests = n
	}
}

const (
	stressMaxNS    = 3
	stressMaxRes   = 5
	stressResTypes = 5
)

// TestStress spawns 1000 parallel subtests that each acquire an instance,
// create random namespaces and resources, verify them, and release.
func TestStress(t *testing.T) {
	t.Parallel()

	for i := range stressSubtests {
		t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			rng := rand.New(rand.NewPCG(uint64(i), 0)) //nolint:gosec // deterministic PRNG for reproducibility

			inst, client := acquireWithClient(ctx, t, sharedManager)
			defer inst.Release( //nolint:errcheck // safe to ignore in defer; on failure instance is removed from pool
				false,
			)

			stressVerifyCleanInstance(ctx, t, client)

			nsCount := rng.IntN(stressMaxNS) + 1
			namespaces := make([]string, 0, nsCount)

			for n := range nsCount {
				nsName := uniqueNS("stress")
				stressCreateNamespace(ctx, t, client, nsName)
				namespaces = append(namespaces, nsName)

				resCount := rng.IntN(stressMaxRes) + 1
				for r := range resCount {
					idx := n*stressMaxRes + r
					stressCreateRandomResource(ctx, t, client, nsName, idx, rng)
				}
			}

			for _, ns := range namespaces {
				stressDeleteNamespace(ctx, t, client, ns)
			}
		})
	}
}

func stressCreateNamespace(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) {
	t.Helper()

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}

	created, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace %s: %v", name, err)
	}

	if created.Name != name {
		t.Fatalf("Namespace name mismatch: want %s, got %s", name, created.Name)
	}
}

func stressDeleteNamespace(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) {
	t.Helper()

	if err := client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		t.Logf("Warning: failed to delete namespace %s: %v", name, err)
	}
}

func stressCreateRandomResource(
	ctx context.Context,
	t *testing.T,
	client kubernetes.Interface,
	ns string,
	idx int,
	rng *rand.Rand,
) {
	t.Helper()

	switch rng.IntN(stressResTypes) {
	case 0:
		stressCreateConfigMap(ctx, t, client, ns, idx)
	case 1:
		stressCreateSecret(ctx, t, client, ns, idx)
	case 2:
		stressCreateService(ctx, t, client, ns, idx)
	case 3:
		stressCreatePod(ctx, t, client, ns, idx)
	case 4:
		stressCreateServiceAccount(ctx, t, client, ns, idx)
	}
}

func stressCreateConfigMap(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-cm-%d", idx)
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       map[string]string{"key": fmt.Sprintf("value-%d", idx)},
	}

	err := retry.OnError(retry.DefaultBackoff, errors.IsNotFound, func() error {
		created, createErr := client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
		if createErr != nil {
			return createErr //nolint:wrapcheck // retry.OnError needs unwrapped error for IsNotFound check
		}
		if created.Name != name {
			t.Fatalf("ConfigMap name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create ConfigMap %s/%s: %v", ns, name, err)
	}
}

func stressCreateSecret(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-secret-%d", idx)
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		StringData: map[string]string{"secret": fmt.Sprintf("val-%d", idx)},
	}

	err := retry.OnError(retry.DefaultBackoff, errors.IsNotFound, func() error {
		created, createErr := client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
		if createErr != nil {
			return createErr //nolint:wrapcheck // retry.OnError needs unwrapped error for IsNotFound check
		}
		if created.Name != name {
			t.Fatalf("Secret name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create Secret %s/%s: %v", ns, name, err)
	}
}

func stressCreateService(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-svc-%d", idx)
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:     int32(8080 + idx%1000), //nolint:gosec // idx is bounded by stressMaxNS*stressMaxRes (15)
					Protocol: v1.ProtocolTCP,
				},
			},
			ClusterIP: "None",
		},
	}

	err := retry.OnError(retry.DefaultBackoff, errors.IsNotFound, func() error {
		created, createErr := client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{})
		if createErr != nil {
			return createErr //nolint:wrapcheck // retry.OnError needs unwrapped error for IsNotFound check
		}
		if created.Name != name {
			t.Fatalf("Service name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create Service %s/%s: %v", ns, name, err)
	}
}

func stressCreatePod(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
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

	err := retry.OnError(retry.DefaultBackoff, errors.IsNotFound, func() error {
		created, createErr := client.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
		if createErr != nil {
			return createErr //nolint:wrapcheck // retry.OnError needs unwrapped error for IsNotFound check
		}
		if created.Name != name {
			t.Fatalf("Pod name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create Pod %s/%s: %v", ns, name, err)
	}
}

func stressCreateServiceAccount(ctx context.Context, t *testing.T, client kubernetes.Interface, ns string, idx int) {
	t.Helper()

	name := fmt.Sprintf("stress-sa-%d", idx)
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}

	err := retry.OnError(retry.DefaultBackoff, errors.IsNotFound, func() error {
		created, createErr := client.CoreV1().ServiceAccounts(ns).Create(ctx, sa, metav1.CreateOptions{})
		if createErr != nil {
			return createErr //nolint:wrapcheck // retry.OnError needs unwrapped error for IsNotFound check
		}
		if created.Name != name {
			t.Fatalf("ServiceAccount name mismatch: want %s, got %s", name, created.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount %s/%s: %v", ns, name, err)
	}
}

func stressVerifyCleanInstance(ctx context.Context, t *testing.T, client kubernetes.Interface) {
	t.Helper()

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}

	for idx := range nsList.Items {
		ns := nsList.Items[idx]
		if _, ok := systemNamespaces[ns.Name]; !ok {
			t.Fatalf("Instance not clean: found unexpected namespace %q", ns.Name)
		}
	}
}
