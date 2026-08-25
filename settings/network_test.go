package settings

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

func TestWithNoProxyAndCustomCAWorkForRealRequests(t *testing.T) {
	var proxied atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(writer, "target")
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxied.Add(1)
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		for key, values := range response.Header {
			writer.Header()[key] = values
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, response.Body)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = withNoProxy(http.ProxyURL(proxyURL), target.Listener.Addr().String())
	client := &http.Client{Transport: transport}
	response, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(body) != "target" || proxied.Load() != 0 {
		t.Fatalf("no-proxy response=%q err=%v proxy-count=%d", body, err, proxied.Load())
	}

	proxiedTransport := http.DefaultTransport.(*http.Transport).Clone()
	proxiedTransport.Proxy = http.ProxyURL(proxyURL)
	response, err = (&http.Client{Transport: proxiedTransport}).Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if proxied.Load() != 1 {
		t.Fatalf("proxy-count=%d, want 1", proxied.Load())
	}

	tlsTarget := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, "custom-ca")
	}))
	defer tlsTarget.Close()
	pool := x509.NewCertPool()
	certificate := tlsTarget.Certificate()
	if !pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})) {
		t.Fatal("failed to add test CA")
	}
	tlsTransport := http.DefaultTransport.(*http.Transport).Clone()
	tlsTransport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	tlsResponse, err := (&http.Client{Transport: tlsTransport}).Get(tlsTarget.URL)
	if err != nil {
		t.Fatal(err)
	}
	tlsBody, err := io.ReadAll(tlsResponse.Body)
	_ = tlsResponse.Body.Close()
	if err != nil || !strings.Contains(string(tlsBody), "custom-ca") {
		t.Fatalf("custom-CA response=%q err=%v", tlsBody, err)
	}
}
