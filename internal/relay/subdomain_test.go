package relay

import (
	"regexp"
	"testing"
)

var subdomainRe = regexp.MustCompile(`^[a-z]+-[a-z]+-\d{4}$`)

func TestGenerateSubdomainFormat(t *testing.T) {
	s, err := GenerateSubdomain()
	if err != nil {
		t.Fatalf("GenerateSubdomain: %v", err)
	}
	if !subdomainRe.MatchString(s) {
		t.Fatalf("subdomain %q does not match %s", s, subdomainRe)
	}
}

func TestGenerateSubdomainVariety(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s, err := GenerateSubdomain()
		if err != nil {
			t.Fatalf("GenerateSubdomain: %v", err)
		}
		seen[s] = true
	}
	// With ~tens of thousands of combinations, 100 draws should yield many
	// distinct values. Allow generous slack to avoid flakiness.
	if len(seen) < 90 {
		t.Fatalf("expected high variety, got %d distinct of 100", len(seen))
	}
}
