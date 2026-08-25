package settings

import (
	"net/http"
	"net/url"
	"testing"
)

func TestNoProxyMatchSupportsHostDomainAndPort(t *testing.T) {
	tests := []struct {
		target string
		list   string
		want   bool
	}{
		{"http://cache.example:8080", "cache.example", true},
		{"http://worker.cache.example:8080", ".cache.example", true},
		{"http://cache.example:8080", "cache.example:443", false},
		{"http://cache.example:8080", "*", true},
	}
	for _, test := range tests {
		target, err := url.Parse(test.target)
		if err != nil {
			t.Fatal(err)
		}
		if got := noProxyMatch(target, test.list); got != test.want {
			t.Errorf("noProxyMatch(%q, %q) = %v, want %v", test.target, test.list, got, test.want)
		}
	}
}

func TestWithNoProxyBypassesOnlyMatchingTargets(t *testing.T) {
	proxy, err := url.Parse("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	configured := withNoProxy(func(*http.Request) (*url.URL, error) { return proxy, nil }, "cache.example")
	for _, test := range []struct {
		target string
		want   bool
	}{
		{"http://cache.example:8080", false},
		{"http://other.example:8080", true},
	} {
		target, err := url.Parse(test.target)
		if err != nil {
			t.Fatal(err)
		}
		got, err := configured(&http.Request{URL: target})
		if err != nil || (got != nil) != test.want {
			t.Errorf("withNoProxy(%q) = %v, %v; want proxy=%v", test.target, got, err, test.want)
		}
	}
}
