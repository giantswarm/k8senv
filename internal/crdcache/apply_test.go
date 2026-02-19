package crdcache

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestApplyMissingKindErrSubstring verifies that the upstream
// runtime.missingKindErr error message still contains the substring
// used by isMissingKindDecodeError for its string-based fallback.
//
// The upstream missingKindErr type is unexported and its Error() method
// produces: "Object 'Kind' is missing in '<data>'". We match against the
// prefix "Object 'Kind' is missing" because the suffix varies by input.
//
// If this test fails, the upstream error message format has changed and
// missingKindErrSubstring must be updated.
func TestApplyMissingKindErrSubstring(t *testing.T) {
	t.Parallel()
	err := runtime.NewMissingKindErr("test-data")

	if !strings.Contains(err.Error(), missingKindErrSubstring) {
		t.Fatalf(
			"upstream missingKindErr message %q no longer contains expected substring %q; update missingKindErrSubstring",
			err.Error(),
			missingKindErrSubstring,
		)
	}
}

// TestApplyIsMissingKindDecodeError exercises the isMissingKindDecodeError
// function against error types that arise from the upstream Kubernetes
// YAML/JSON decode pipeline.
//
// String-based detection context: the upstream k8s libraries wrap
// missingKindErr through multiple paths without implementing Unwrap:
//   - YAML path: YAMLSyntaxError{err: missingKindErr} (no Unwrap)
//   - JSON path: fmt.Errorf("...%s", missingKindErr) (no %w verb)
//
// This makes runtime.IsMissingKind fail on wrapped errors, requiring a
// fallback to string matching on missingKindErrSubstring.
func TestApplyIsMissingKindDecodeError(t *testing.T) {
	t.Parallel()
	// Note: isMissingKindDecodeError does not handle nil errors.
	// The caller (applyDocument) only invokes it when err != nil.
	tests := map[string]struct {
		err  error
		want bool
	}{
		"direct missingKindErr via NewMissingKindErr": {
			err:  runtime.NewMissingKindErr("test-data"),
			want: true,
		},
		"wrapped without Unwrap (simulates YAMLSyntaxError)": {
			// YAMLSyntaxError embeds the error in a struct field
			// and delegates Error() to the inner error, but does
			// not implement Unwrap. This means runtime.IsMissingKind
			// cannot reach the inner *missingKindErr via type assertion.
			err:  noUnwrapError{inner: runtime.NewMissingKindErr("yaml-input")},
			want: true,
		},
		"wrapped via fmt.Errorf without %w (simulates JSON path)": {
			// The JSON decode path wraps missingKindErr using string
			// formatting (not %w), embedding the message but losing
			// the type information entirely.
			//nolint:errorlint // intentionally using %s to simulate upstream behavior that loses type info
			err:  fmt.Errorf("error unmarshaling JSON: %s", runtime.NewMissingKindErr("json-input")),
			want: true,
		},
		"wrapped via fmt.Errorf with %w (preserves type)": {
			// When properly wrapped with %w, runtime.IsMissingKind
			// can unwrap and find the type. This path currently works
			// via the typed check, not the string fallback.
			err:  fmt.Errorf("decode error: %w", runtime.NewMissingKindErr("wrapped")),
			want: true,
		},
		"unrelated error": {
			err:  errors.New("connection refused"),
			want: false,
		},
		"error containing partial substring": {
			// An error that contains "Kind" but not the full substring
			// should not match.
			err:  errors.New("missing Kind field in resource"),
			want: false,
		},
		"error containing exact substring in different context": {
			// An error from a different source that happens to contain
			// the same substring should still be detected. This is an
			// accepted trade-off of string-based detection.
			err:  fmt.Errorf("validation failed: %s", missingKindErrSubstring),
			want: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := isMissingKindDecodeError(tc.err)
			if got != tc.want {
				t.Errorf("isMissingKindDecodeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestApplyMissingKindErrSubstringValue documents the exact substring being
// matched. If this test fails, someone changed the constant without updating
// the tests.
func TestApplyMissingKindErrSubstringValue(t *testing.T) {
	t.Parallel()
	const expected = "Object 'Kind' is missing"
	if missingKindErrSubstring != expected {
		t.Errorf("missingKindErrSubstring = %q, want %q", missingKindErrSubstring, expected)
	}
}

// TestApplyRuntimeIsMissingKindTypedCheck verifies that runtime.IsMissingKind
// works for direct missingKindErr instances but fails for wrapped instances
// without Unwrap. This documents why the string-based fallback exists.
func TestApplyRuntimeIsMissingKindTypedCheck(t *testing.T) {
	t.Parallel()
	t.Run("direct error passes typed check", func(t *testing.T) {
		t.Parallel()
		err := runtime.NewMissingKindErr("test")
		if !runtime.IsMissingKind(err) {
			t.Fatal("runtime.IsMissingKind should return true for direct missingKindErr")
		}
	})

	t.Run("wrapped without Unwrap fails typed check", func(t *testing.T) {
		t.Parallel()
		inner := runtime.NewMissingKindErr("test")
		wrapped := noUnwrapError{inner: inner}

		if runtime.IsMissingKind(wrapped) {
			t.Fatal(
				"runtime.IsMissingKind should return false for wrapped error without Unwrap; if this starts passing, the string-based fallback may no longer be needed",
			)
		}
	})

	t.Run("our function catches what runtime.IsMissingKind misses", func(t *testing.T) {
		t.Parallel()
		inner := runtime.NewMissingKindErr("test")
		wrapped := noUnwrapError{inner: inner}

		if runtime.IsMissingKind(wrapped) {
			t.Skip("runtime.IsMissingKind now handles this case; string fallback may be removable")
		}
		if !isMissingKindDecodeError(wrapped) {
			t.Fatal("isMissingKindDecodeError should catch wrapped missingKindErr via string fallback")
		}
	})
}

// noUnwrapError simulates upstream wrappers like YAMLSyntaxError that embed
// an inner error and delegate Error() but do not implement Unwrap().
type noUnwrapError struct {
	inner error
}

func (e noUnwrapError) Error() string {
	return e.inner.Error()
}

// TestApplyParseFileDocuments exercises parseFileDocuments against single-doc,
// multi-doc, empty, and malformed YAML inputs. The function is a pure parser
// with no I/O — it decodes []byte content into parsedDoc slices.
func TestApplyParseFileDocuments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content  string
		wantDocs int
		wantErr  bool
		wantKind string // expected Kind of the first document (empty = skip check)
	}{
		"single ConfigMap": {
			content:  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test-cm",
			wantDocs: 1,
			wantKind: "ConfigMap",
		},
		"single CRD": {
			content:  "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: foos.example.com",
			wantDocs: 1,
			wantKind: "CustomResourceDefinition",
		},
		"multi-document YAML": {
			content:  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: s1",
			wantDocs: 2,
			wantKind: "ConfigMap",
		},
		"three documents": {
			content:  "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ns1\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: dep1",
			wantDocs: 3,
			wantKind: "Namespace",
		},
		"empty content returns zero docs": {
			content:  "",
			wantDocs: 0,
		},
		"whitespace-only content returns zero docs": {
			content:  "   \n\n\t  \n",
			wantDocs: 0,
		},
		"separator-only content returns error": {
			// The YAML reader splits on "---" separators. Consecutive separators
			// produce non-empty documents (e.g., "---") that fail decoding
			// because they lack a kind field.
			content: "---\n---\n---",
			wantErr: true,
		},
		"valid docs with blank docs between separators": {
			content:  "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n---\n\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: s1\n---",
			wantDocs: 2,
			wantKind: "ConfigMap",
		},
		"leading separator before document": {
			content:  "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1",
			wantDocs: 1,
			wantKind: "ConfigMap",
		},
		"document with all metadata fields": {
			content:  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n  namespace: kube-system\n  labels:\n    app: test\ndata:\n  key: value",
			wantDocs: 1,
			wantKind: "ConfigMap",
		},
		"JSON document": {
			content:  `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"json-cm"}}`,
			wantDocs: 1,
			wantKind: "ConfigMap",
		},
		"missing kind field returns error": {
			content: "apiVersion: v1\nmetadata:\n  name: no-kind",
			wantErr: true,
		},
		"empty object returns error": {
			content: "foo: bar",
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			docs, err := parseFileDocuments([]byte(tc.content), "test.yaml")
			assertParsedDocs(t, docs, err, tc.wantDocs, tc.wantErr, tc.wantKind)
		})
	}
}

// assertParsedDocs validates parseFileDocuments output against expected values.
func assertParsedDocs(t *testing.T, docs []parsedDoc, err error, wantDocs int, wantErr bool, wantKind string) {
	t.Helper()

	if wantErr {
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != wantDocs {
		t.Fatalf("got %d docs, want %d", len(docs), wantDocs)
	}
	if wantKind != "" && len(docs) > 0 {
		got := docs[0].obj.GroupVersionKind().Kind
		if got != wantKind {
			t.Errorf("first doc kind = %q, want %q", got, wantKind)
		}
	}
}

// TestApplyParseFileDocumentsMissingKindSentinel verifies that parseFileDocuments
// wraps ErrMissingKind as a sentinel error detectable via errors.Is.
func TestApplyParseFileDocumentsMissingKindSentinel(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content string
	}{
		"no kind field": {
			content: "apiVersion: v1\nmetadata:\n  name: test",
		},
		"kind is empty string": {
			// An object with kind="" is treated the same as missing.
			content: "apiVersion: v1\nkind: \"\"\nmetadata:\n  name: test",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseFileDocuments([]byte(tc.content), "test.yaml")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrMissingKind) {
				t.Errorf("error %q does not wrap ErrMissingKind", err)
			}
		})
	}
}

// TestApplyParseFileDocumentsFilename verifies that parseFileDocuments propagates
// the file parameter into every returned parsedDoc.
func TestApplyParseFileDocumentsFilename(t *testing.T) {
	t.Parallel()
	content := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: s1"

	docs, err := parseFileDocuments([]byte(content), "crds/my-crd.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, doc := range docs {
		if doc.file != "crds/my-crd.yaml" {
			t.Errorf("docs[%d].file = %q, want %q", i, doc.file, "crds/my-crd.yaml")
		}
	}
}

// TestApplyIsCRDDocument exercises isCRDDocument against CRD and non-CRD
// unstructured objects to verify group and kind matching.
func TestApplyIsCRDDocument(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		apiVersion string
		kind       string
		want       bool
	}{
		"CRD v1": {
			apiVersion: "apiextensions.k8s.io/v1",
			kind:       "CustomResourceDefinition",
			want:       true,
		},
		"CRD v1beta1": {
			apiVersion: "apiextensions.k8s.io/v1beta1",
			kind:       "CustomResourceDefinition",
			want:       true,
		},
		"ConfigMap is not CRD": {
			apiVersion: "v1",
			kind:       "ConfigMap",
			want:       false,
		},
		"Deployment is not CRD": {
			apiVersion: "apps/v1",
			kind:       "Deployment",
			want:       false,
		},
		"correct group wrong kind": {
			apiVersion: "apiextensions.k8s.io/v1",
			kind:       "SomethingElse",
			want:       false,
		},
		"correct kind wrong group": {
			apiVersion: "example.com/v1",
			kind:       "CustomResourceDefinition",
			want:       false,
		},
		"empty apiVersion and kind": {
			apiVersion: "",
			kind:       "",
			want:       false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion(tc.apiVersion)
			obj.SetKind(tc.kind)

			got := isCRDDocument(obj)
			if got != tc.want {
				t.Errorf("isCRDDocument(apiVersion=%q, kind=%q) = %v, want %v",
					tc.apiVersion, tc.kind, got, tc.want)
			}
		})
	}
}
