package crdcache

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/giantswarm/k8senv/internal/sentinel"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// ErrMissingKind is returned when a YAML document lacks a 'kind' field.
const ErrMissingKind = sentinel.Error("missing kind in document")

// ErrTooManyYAMLFiles is returned when a CRD directory contains more files than maxYAMLFiles.
const ErrTooManyYAMLFiles = sentinel.Error("too many YAML files in directory")

const (
	// discoveryRetryCount is the number of attempts for CRD registration propagation.
	discoveryRetryCount = 5

	// discoveryRetryDelay is the wait time between discovery retry attempts.
	// 100ms strikes a balance between responsiveness and avoiding unnecessary
	// CPU cycles polling the localhost API server for CRD registration.
	discoveryRetryDelay = 100 * time.Millisecond

	// yamlDecoderBufferSize is the initial buffer size in bytes for the
	// YAML/JSON decoder used when parsing CRD documents.
	yamlDecoderBufferSize = 4096

	// maxYAMLFiles is the upper bound on the number of YAML files that
	// applyYAMLFiles will process. This guards against misconfigured
	// directories containing an unreasonable number of files.
	maxYAMLFiles = 1000
)

// discoveryMapper caches a RESTMapper built from live API server discovery,
// avoiding redundant GetAPIGroupResources round-trips when multiple YAML
// documents share already-known GVKs (e.g., apiextensions.k8s.io/v1 CRDs).
// The mapper is refreshed on demand when a NoKindMatch error indicates
// the cache is stale (e.g., after applying a CRD that registers a new type).
type discoveryMapper struct {
	mapper     meta.RESTMapper
	discClient discovery.DiscoveryInterface
}

// newDiscoveryMapper creates a discoveryMapper with an eagerly populated cache.
// discClient must be a non-caching discovery client so that refresh() observes
// freshly registered CRDs via live API server requests.
func newDiscoveryMapper(discClient discovery.DiscoveryInterface) (*discoveryMapper, error) {
	dm := &discoveryMapper{discClient: discClient}
	if err := dm.refresh(); err != nil {
		return nil, err
	}
	return dm, nil
}

// refresh rebuilds the cached RESTMapper from live API server discovery.
func (dm *discoveryMapper) refresh() error {
	gr, err := restmapper.GetAPIGroupResources(dm.discClient)
	if err != nil {
		return fmt.Errorf("get api groups: %w", err)
	}
	dm.mapper = restmapper.NewDiscoveryRESTMapper(gr)
	return nil
}

// applyYAMLFiles applies pre-read YAML files to the cluster.
// Files are processed in the order provided. Multi-document YAML files are supported.
// The files slice must be pre-populated by the caller (typically from computeDirHash),
// which reads file contents during hashing to avoid redundant disk reads.
func applyYAMLFiles(
	ctx context.Context,
	logger *slog.Logger,
	restCfg *rest.Config,
	dirPath string,
	files []hashedFile,
) error {
	if len(files) > maxYAMLFiles {
		return fmt.Errorf("%w: found %d files (max %d)", ErrTooManyYAMLFiles, len(files), maxYAMLFiles)
	}

	// Create dynamic client
	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("create dynamic client: %w", err)
	}

	// Create a non-caching discovery client for REST mapping. A non-caching
	// client is required so that discoveryMapper.refresh can observe freshly
	// registered CRDs on each retry attempt via live API server requests.
	discClient, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("create discovery client: %w", err)
	}

	// Build REST mapper once upfront and reuse across all documents.
	// For CRD definitions (the primary use case), apiextensions.k8s.io/v1
	// is always pre-registered, so the cached mapper resolves without refresh.
	// For custom resources that depend on freshly applied CRDs, the
	// NoKindMatch → refresh → retry path in discoverRESTMapping handles staleness.
	dm, err := newDiscoveryMapper(discClient)
	if err != nil {
		return fmt.Errorf("build rest mapper: %w", err)
	}

	// Apply each file
	for _, f := range files {
		relPath, relErr := filepath.Rel(dirPath, f.path)
		if relErr != nil {
			// Fall back to absolute path if relative path cannot be computed
			relPath = f.path
		}
		logger.Debug("applying file", "file", relPath)
		if applyErr := applyFileContent(ctx, logger, dynClient, dm, f.content); applyErr != nil {
			return fmt.Errorf("apply %s: %w", relPath, applyErr)
		}
	}

	return nil
}

// applyFileContent applies the content of a single YAML file (may contain multiple documents).
// The content is provided directly to avoid redundant disk reads, since it was already
// read during hash computation.
func applyFileContent(
	ctx context.Context,
	logger *slog.Logger,
	dynClient dynamic.Interface,
	dm *discoveryMapper,
	content []byte,
) error {
	// Split multi-document YAML
	reader := yamlutil.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))

	docNum := 0
	for {
		docNum++
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read yaml doc %d: %w", docNum, err)
		}

		// Skip empty documents
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		if err := applyDocument(ctx, logger, dynClient, dm, doc); err != nil {
			return fmt.Errorf("doc %d: %w", docNum, err)
		}
	}

	return nil
}

// applyDocument applies a single YAML document to the cluster.
func applyDocument(
	ctx context.Context,
	logger *slog.Logger,
	dynClient dynamic.Interface,
	dm *discoveryMapper,
	doc []byte,
) error {
	// Decode to unstructured
	obj := &unstructured.Unstructured{}
	dec := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(doc), yamlDecoderBufferSize)
	if err := dec.Decode(obj); err != nil {
		if isMissingKindDecodeError(err) {
			return fmt.Errorf("decode yaml: %w", ErrMissingKind)
		}
		return fmt.Errorf("decode yaml: %w", err)
	}

	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" {
		return ErrMissingKind
	}

	// Resolve REST mapping using cached mapper; refreshes on NoKindMatch.
	mapping, err := discoverRESTMapping(ctx, logger, dm, gvk)
	if err != nil {
		return err
	}

	// Determine if namespaced or cluster-scoped
	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "default"
		}
		dr = dynClient.Resource(mapping.Resource).Namespace(ns)
	} else {
		dr = dynClient.Resource(mapping.Resource)
	}

	// Create the resource
	_, err = dr.Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}

	logger.Debug("created resource", "kind", gvk.Kind, "name", obj.GetName())
	return nil
}

// missingKindErrSubstring is the distinctive prefix produced by
// runtime.missingKindErr.Error(). Used as a last-resort string check when
// typed error unwrapping fails because upstream wrappers (YAMLSyntaxError,
// JSON unmarshaler) do not implement Unwrap and use unexported inner fields.
const missingKindErrSubstring = "Object 'Kind' is missing"

// isMissingKindDecodeError reports whether a decode error indicates a missing
// 'kind' field. It tries typed checks first, then falls back to string
// matching because the upstream k8s libraries wrap missingKindErr through
// multiple paths (YAML via YAMLSyntaxError, JSON via "error unmarshaling
// JSON") without implementing Unwrap or exporting the error type.
func isMissingKindDecodeError(err error) bool {
	// Preferred: typed check from the runtime package itself.
	if runtime.IsMissingKind(err) {
		return true
	}

	// Fallback: string match. The upstream wrappers (YAMLSyntaxError for YAML
	// input, fmt.Errorf for JSON input) both embed missingKindErr's message
	// without Unwrap support, so typed unwrapping cannot reach it. Check the
	// full error string for the distinctive missingKindErr prefix.
	return strings.Contains(err.Error(), missingKindErrSubstring)
}

// discoverRESTMapping resolves the REST mapping for a GVK using the cached
// mapper. If the cached mapper returns a NoKindMatch/NoResourceMatch error
// (indicating a recently applied CRD hasn't propagated yet), the mapper is
// refreshed via live API server discovery and the lookup is retried with
// backoff to allow CRD registration to propagate.
//
// IMPORTANT: dm.discClient must be a non-caching discovery client (e.g., one
// created via discovery.NewDiscoveryClientForConfig). Each refresh call issues
// fresh HTTP requests to the API server. A caching client would serve stale
// results, making retries unable to detect newly registered CRDs.
func discoverRESTMapping(
	ctx context.Context,
	logger *slog.Logger,
	dm *discoveryMapper,
	gvk schema.GroupVersionKind,
) (*meta.RESTMapping, error) {
	// Fast path: try the cached mapper without any HTTP calls.
	mapping, err := dm.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err == nil {
		return mapping, nil
	}

	// Return immediately on non-NoMatch errors (e.g., ambiguous resources).
	if !meta.IsNoMatchError(err) {
		return nil, fmt.Errorf("get rest mapping for %v: %w", gvk, err)
	}

	// Slow path: cached mapper doesn't know this GVK. Refresh and retry
	// with backoff to allow CRD registration to propagate.
	var lastErr error
	for attempt := range discoveryRetryCount {
		if err := ctx.Err(); err != nil {
			return nil, contextCauseErr(ctx, gvk)
		}

		if err := dm.refresh(); err != nil {
			return nil, err
		}

		mapping, err := dm.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err == nil {
			return mapping, nil
		}

		// Only retry on NoKindMatchError/NoResourceMatchError (CRD not yet established).
		// Return immediately on other errors (e.g., ambiguous resources, API failures).
		if !meta.IsNoMatchError(err) {
			return nil, fmt.Errorf("get rest mapping for %v: %w", gvk, err)
		}
		lastErr = err

		if attempt < discoveryRetryCount-1 {
			logger.Debug("waiting for CRD registration", "gvk", gvk, "attempt", attempt+1)
			retryTimer := time.NewTimer(discoveryRetryDelay)
			select {
			case <-ctx.Done():
				retryTimer.Stop()
				return nil, contextCauseErr(ctx, gvk)
			case <-retryTimer.C:
			}
		}
	}

	// Prefer context error if the context was canceled during retries.
	if ctx.Err() != nil {
		return nil, contextCauseErr(ctx, gvk)
	}
	return nil, fmt.Errorf("get rest mapping for %v: %w", gvk, lastErr)
}

// contextCauseErr returns a context cancellation error for REST mapping
// resolution of the given GVK. It wraps context.Cause to provide a
// consistent error message across all cancellation check sites.
func contextCauseErr(ctx context.Context, gvk schema.GroupVersionKind) error {
	return fmt.Errorf("get rest mapping for %v: %w", gvk, context.Cause(ctx))
}
