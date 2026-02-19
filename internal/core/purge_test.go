package core

import (
	"strings"
	"testing"
)

func TestIsValidDNSLabel(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want bool
	}{
		// Valid labels.
		"simple lowercase":             {name: "default", want: true},
		"with hyphens":                 {name: "kube-system", want: true},
		"single char":                  {name: "a", want: true},
		"single digit":                 {name: "1", want: true},
		"digits only":                  {name: "123", want: true},
		"alphanumeric mixed":           {name: "test-ns-42", want: true},
		"max length 63":                {name: strings.Repeat("a", 63), want: true},
		"starts with digit":            {name: "0abc", want: true},
		"ends with digit":              {name: "abc0", want: true},
		"multiple consecutive hyphens": {name: "a--b", want: true},

		// Invalid labels.
		"empty string":            {name: "", want: false},
		"too long 64":             {name: strings.Repeat("a", 64), want: false},
		"leading hyphen":          {name: "-abc", want: false},
		"trailing hyphen":         {name: "abc-", want: false},
		"uppercase letter":        {name: "Abc", want: false},
		"contains slash":          {name: "ns/evil", want: false},
		"contains percent":        {name: "ns%evil", want: false},
		"contains underscore":     {name: "ns_evil", want: false},
		"contains dot":            {name: "ns.evil", want: false},
		"contains space":          {name: "ns evil", want: false},
		"contains backslash":      {name: `ns\evil`, want: false},
		"single hyphen":           {name: "-", want: false},
		"only hyphens":            {name: "---", want: false},
		"unicode letter":          {name: "caf\u00e9", want: false},
		"null byte":               {name: "ns\x00evil", want: false},
		"sql injection semicolon": {name: "ns;DROP TABLE kine", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := isValidDNSLabel(tc.name)
			if got != tc.want {
				t.Errorf("isValidDNSLabel(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
