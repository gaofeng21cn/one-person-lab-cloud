package build

import "testing"

// The Build worker reads loopback fixtures over plain HTTP and writes its
// immutable output to a real HTTPS registry in the same run, so the transport
// must be decided per host rather than by one global switch.
func TestRunnerRegistrySchemeIsPerHost(t *testing.T) {
	cases := []struct {
		name     string
		runner   Runner
		host     string
		expected string
	}{
		{
			name:     "explicit insecure host uses http",
			runner:   Runner{InsecureHTTPHosts: []string{"127.0.0.1:5000"}},
			host:     "127.0.0.1:5000",
			expected: "http",
		},
		{
			name:     "unlisted host stays https beside an insecure fixture",
			runner:   Runner{AllowHTTP: true, InsecureHTTPHosts: []string{"127.0.0.1:5000"}},
			host:     "uswccr.ccs.tencentyun.com",
			expected: "https",
		},
		{
			name:     "empty list keeps the legacy AllowHTTP behaviour",
			runner:   Runner{AllowHTTP: true},
			host:     "127.0.0.1:5000",
			expected: "http",
		},
		{
			name:     "production default stays https",
			runner:   Runner{},
			host:     "uswccr.ccs.tencentyun.com",
			expected: "https",
		},
	}
	for _, tc := range cases {
		if got := tc.runner.scheme(tc.host); got != tc.expected {
			t.Fatalf("%s: scheme(%q)=%q want %q", tc.name, tc.host, got, tc.expected)
		}
	}
}

func TestRunnerRegistryURLKeepsRepositoryPath(t *testing.T) {
	runner := Runner{AllowHTTP: true, InsecureHTTPHosts: []string{"127.0.0.1:5000"}}
	if got := runner.registryURL("127.0.0.1:5000/result", "manifests", "sha256:abc"); got != "http://127.0.0.1:5000/v2/result/manifests/sha256:abc" {
		t.Fatalf("fixture url=%q", got)
	}
	if got := runner.registryURL("uswccr.ccs.tencentyun.com/oplcloud/tenant-abc", "manifests", "sha256:abc"); got != "https://uswccr.ccs.tencentyun.com/v2/oplcloud/tenant-abc/manifests/sha256:abc" {
		t.Fatalf("real registry url=%q", got)
	}
}
