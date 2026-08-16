package validator

import (
	"errors"
	"net"
	"testing"
)

func TestValidateOutboundURL(t *testing.T) {
	// Resolve every hostname to a public address so that the hostname cases
	// exercise URL parsing rather than live DNS.
	originalResolver := resolveHost
	t.Cleanup(func() { resolveHost = originalResolver })
	resolveHost = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"public https host", "https://api.example.com/v1", false},
		{"public https host with port", "https://api.example.com:8443/v1", false},
		{"public literal ip", "https://93.184.216.34/v1", false},

		// Internal and metadata addresses that must never be reachable.
		{"aws imds", "https://169.254.169.254/latest/meta-data/", true},
		{"gcp style metadata ip", "https://169.254.169.254/computeMetadata/v1/", true},
		{"ipv4 mapped imds", "https://[::ffff:169.254.169.254]/", true},
		{"loopback", "https://127.0.0.1/", true},
		{"loopback ipv6", "https://[::1]/", true},
		{"rfc1918 ten", "https://10.0.0.5/", true},
		{"rfc1918 one seven two", "https://172.16.0.5/", true},
		{"rfc1918 one nine two", "https://192.168.1.5/", true},
		{"unspecified", "https://0.0.0.0/", true},
		{"carrier grade nat", "https://100.64.0.1/", true},
		{"ietf protocol assignment", "https://192.0.0.192/", true},

		// IPv6 forms that carry an IPv4 target To4 does not unwrap. Judged by the
		// address they actually route to, not by the outer IPv6 address.
		{"nat64 rfc1918", "https://[64:ff9b::a00:5]/", true},
		{"nat64 imds", "https://[64:ff9b::a9fe:a9fe]/", true},
		{"6to4 rfc1918", "https://[2002:a00:5::]/", true},
		{"6to4 imds", "https://[2002:a9fe:a9fe::]/", true},
		{"nat64 public target allowed", "https://[64:ff9b::5db8:d822]/", false},

		// 64:ff9b:1::/48 is the RFC 8215 local-use range. RFC 6052 splits the
		// embedded address for prefixes shorter than /96, so the final four bytes
		// are not the destination: this address translates to 10.0.0.5 while its
		// suffix reads as 8.8.8.8. The whole range is refused rather than decoded.
		{"nat64 local use split encoding", "https://[64:ff9b:1:a00:0:5:808:808]/", true},
		{"nat64 local use zero suffix", "https://[64:ff9b:1::]/", true},
		{"nat64 local use public looking suffix", "https://[64:ff9b:1::5db8:d822]/", true},

		// Reserved IPv4 ranges that are not a real destination.
		{"benchmarking range", "https://198.18.0.1/", true},
		{"benchmarking range upper", "https://198.19.255.254/", true},
		{"reserved future use", "https://240.0.0.1/", true},
		{"broadcast", "https://255.255.255.255/", true},
		{"test net one", "https://192.0.2.1/", true},
		{"test net two", "https://198.51.100.1/", true},
		{"test net three", "https://203.0.113.1/", true},

		{"http scheme rejected", "http://api.example.com/", true},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"gopher scheme rejected", "gopher://api.example.com/", true},
		{"relative url rejected", "/v1/endpoint", true},
		{"empty rejected", "", true},
		{"whitespace rejected", "   ", true},
		{"credentials rejected", "https://user:pass@api.example.com/", true},
		{"missing host rejected", "https:///path", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutboundURL(tt.url)
			if tt.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected, got nil error", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected %q to be allowed, got: %v", tt.url, err)
			}
		})
	}
}

// A hostname resolving to a mix of public and internal addresses must be
// rejected: allowing it would let a DNS record smuggle an internal target past
// a check that only looked at the first answer.
func TestValidateOutboundURLRejectsMixedResolution(t *testing.T) {
	originalResolver := resolveHost
	t.Cleanup(func() { resolveHost = originalResolver })
	resolveHost = func(host string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("93.184.216.34"),
			net.ParseIP("169.254.169.254"),
		}, nil
	}

	err := ValidateOutboundURL("https://rebind.example.com/")
	if err == nil {
		t.Fatal("expected mixed public/internal resolution to be rejected")
	}
	if !errors.Is(err, ErrURLNotPublic) {
		t.Fatalf("expected ErrURLNotPublic, got: %v", err)
	}
}

func TestValidateOutboundURLRejectsUnresolvableHost(t *testing.T) {
	originalResolver := resolveHost
	t.Cleanup(func() { resolveHost = originalResolver })
	resolveHost = func(host string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	}

	if err := ValidateOutboundURL("https://does-not-exist.example/"); err == nil {
		t.Fatal("expected unresolvable host to be rejected")
	}
}
