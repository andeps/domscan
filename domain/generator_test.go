package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestGenerateDomains(t *testing.T) {
	domains, err := Generate(Options{
		Keywords: []string{"Nova", "Lab"}, TLDs: []string{".com", "io"},
		MinLength: 3, MaxLength: 8, DigitMode: "forbid", Hyphen: true, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"lab.com", "nova.io", "labnova.com", "nova-lab.io"} {
		if !slices.Contains(domains, wanted) {
			t.Errorf("missing %s in %v", wanted, domains)
		}
	}
	for _, domain := range domains {
		if strings.ContainsAny(domain, "0123456789") {
			t.Errorf("forbidden digit in %s", domain)
		}
	}
}

func TestGenerateDomainsRequiresDigitsAndLimit(t *testing.T) {
	domains, err := Generate(Options{Keywords: []string{"go"}, TLDs: []string{"dev"}, MinLength: 3, MaxLength: 5, DigitMode: "require", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 5 {
		t.Fatalf("got %d domains, want 5", len(domains))
	}
	for _, domain := range domains {
		if !strings.ContainsAny(strings.Split(domain, ".")[0], "0123456789") {
			t.Errorf("digit required in %s", domain)
		}
	}
}

func TestGenerateDomainsWithoutKeywordsEnumeratesFromA(t *testing.T) {
	domains, err := Generate(Options{TLDs: []string{"com"}, MinLength: 1, MaxLength: 2, DigitMode: "forbid", Limit: 30})
	if err != nil {
		t.Fatal(err)
	}
	if domains[0] != "a.com" || domains[25] != "z.com" || domains[26] != "aa.com" || domains[29] != "ad.com" {
		t.Fatalf("unexpected automatic sequence: %v", domains)
	}
}

func TestGenerateDomainsWithoutKeywordsHonorsTLDsAndRequiredDigits(t *testing.T) {
	domains, err := Generate(Options{TLDs: []string{"com", "cn"}, MinLength: 2, MaxLength: 2, DigitMode: "require", Limit: 6})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a0.com", "a0.cn", "a1.com", "a1.cn", "a2.com", "a2.cn"}
	if !slices.Equal(domains, want) {
		t.Fatalf("got %v, want %v", domains, want)
	}
}

func TestGenerateDomainsSupportsStableOffset(t *testing.T) {
	domains, err := Generate(Options{TLDs: []string{"com"}, MinLength: 1, MaxLength: 2, DigitMode: "forbid", Limit: 3, Offset: 26})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"aa.com", "ab.com", "ac.com"}
	if !slices.Equal(domains, want) {
		t.Fatalf("got %v, want %v", domains, want)
	}
}

func TestGenerateDomainsFuzzyPrefixAndSuffix(t *testing.T) {
	prefix, err := Generate(Options{Keywords: []string{"ai"}, TLDs: []string{"com"}, MinLength: 6, MaxLength: 6, DigitMode: "forbid", FuzzyMode: "prefix", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(prefix, []string{"aaaaai.com", "aaabai.com"}) {
		t.Fatalf("prefix result = %v", prefix)
	}
	suffix, err := Generate(Options{Keywords: []string{"ai"}, TLDs: []string{"com"}, MinLength: 6, MaxLength: 6, DigitMode: "forbid", FuzzyMode: "suffix", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(suffix, []string{"aiaaaa.com", "aiaaab.com"}) {
		t.Fatalf("suffix result = %v", suffix)
	}
}

func TestGenerateDomainsFuzzyRequiresDigitsWhenConfigured(t *testing.T) {
	domains, err := Generate(Options{Keywords: []string{"ai"}, TLDs: []string{"com"}, MinLength: 3, MaxLength: 3, DigitMode: "require", FuzzyMode: "suffix", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(domains, []string{"ai0.com", "ai1.com", "ai2.com"}) {
		t.Fatalf("digit fuzzy result = %v", domains)
	}
}

func TestGenerateDomainsValidation(t *testing.T) {
	tests := []Options{
		{Keywords: []string{"中文"}, TLDs: []string{"com"}, MinLength: 1, MaxLength: 3, DigitMode: "forbid"},
		{Keywords: []string{"abc"}, TLDs: []string{}, MinLength: 1, MaxLength: 3, DigitMode: "forbid"},
		{Keywords: []string{"abc"}, TLDs: []string{"com"}, MinLength: 10, MaxLength: 3, DigitMode: "forbid"},
	}
	for i, test := range tests {
		if _, err := Generate(test); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestValidDomain(t *testing.T) {
	for _, domain := range []string{"example.com", "a-b.cn", "x.io"} {
		if !Valid(domain) {
			t.Errorf("expected %s valid", domain)
		}
	}
	for _, domain := range []string{"example", "-bad.com", "bad_.com", "bad..com"} {
		if Valid(domain) {
			t.Errorf("expected %s invalid", domain)
		}
	}
}
