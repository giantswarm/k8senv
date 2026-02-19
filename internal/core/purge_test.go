package core

import (
	"strings"
	"testing"
)

func TestBuildPurgeDeleteQuery(t *testing.T) {
	t.Parallel()

	query, makeArgs := buildPurgeDeleteQuery()

	// The query must start with the baseline-ID filter.
	if !strings.HasPrefix(query, "DELETE FROM kine WHERE id > ?") {
		t.Fatalf("query does not start with expected prefix:\n%s", query)
	}

	// Build args with a sample baseline ID.
	const baselineID int64 = 42
	args := makeArgs(baselineID)

	// Expected: 1 (baselineID) + len(systemNamespaces) exact paths + len(systemNamespaces) LIKE patterns.
	wantArgCount := 1 + len(systemNamespaces)*2
	if len(args) != wantArgCount {
		t.Fatalf("got %d args, want %d", len(args), wantArgCount)
	}

	// First arg is the baseline ID.
	if args[0] != baselineID {
		t.Errorf("args[0] = %v, want %d", args[0], baselineID)
	}

	// Next batch: exact namespace paths (/registry/namespaces/<ns>).
	for i, ns := range systemNamespaces {
		idx := 1 + i
		want := "/registry/namespaces/" + ns
		if args[idx] != want {
			t.Errorf("args[%d] = %v, want %q", idx, args[idx], want)
		}
	}

	// Final batch: LIKE patterns (%/<ns>/%).
	for i, ns := range systemNamespaces {
		idx := 1 + len(systemNamespaces) + i
		want := "%/" + ns + "/%"
		if args[idx] != want {
			t.Errorf("args[%d] = %v, want %q", idx, args[idx], want)
		}
	}

	// Verify the query has the right number of exact-match and LIKE clauses.
	exactCount := strings.Count(query, "AND name != ?")
	if exactCount != len(systemNamespaces) {
		t.Errorf("got %d exact-match clauses, want %d", exactCount, len(systemNamespaces))
	}
	likeCount := strings.Count(query, "AND name NOT LIKE ?")
	if likeCount != len(systemNamespaces) {
		t.Errorf("got %d LIKE clauses, want %d", likeCount, len(systemNamespaces))
	}
}
