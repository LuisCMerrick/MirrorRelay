package security

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRequestClientIPUsesTrustedIngressAddress(t *testing.T) {
	request := httptest.NewRequest("GET", "https://mirror.example/", nil)
	request.RemoteAddr = "127.0.0.1:40123"
	request.Header.Set("X-Real-IP", "203.0.113.44")
	if got := RequestClientIP(request); got != "203.0.113.44" {
		t.Fatalf("client IP = %q", got)
	}
	request.Header.Set("X-Real-IP", "invalid, 127.0.0.1")
	if got := RequestClientIP(request); got != "127.0.0.1" {
		t.Fatalf("invalid ingress address was trusted: %q", got)
	}
}

type targetResolver map[string][]netip.Addr

func (r targetResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

func TestBlockedIP(t *testing.T) {
	values := map[string]bool{
		"127.0.0.1": true, "10.0.0.1": true, "169.254.169.254": true,
		"192.0.2.1": true, "198.51.100.1": true, "203.0.113.1": true,
		"::1": true, "fc00::1": true, "2001:db8::1": true, "3fff::1": true,
		"64:ff9b::7f00:1": true, "2002:7f00:1::1": true,
		"1.1.1.1": false, "2606:4700:4700::1111": false,
	}
	for raw, want := range values {
		if got := IsBlockedIP(netip.MustParseAddr(raw)); got != want {
			t.Errorf("IsBlockedIP(%s)=%v want %v", raw, got, want)
		}
	}
}

func TestResolveApprovedTargetPinsPublicIPAndRetainsTLSHost(t *testing.T) {
	target, err := ResolveApprovedTarget(context.Background(), "https://cdn.example:8443/blob?a=1", false, false, true,
		targetResolver{"cdn.example": {netip.MustParseAddr("8.8.4.4")}})
	if err != nil {
		t.Fatal(err)
	}
	if target.Address != "8.8.4.4:8443" || target.Host != "cdn.example" || target.Authority != "cdn.example:8443" {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestResolveApprovedTargetRejectsPrivateAndMixedDNS(t *testing.T) {
	resolver := targetResolver{"mixed.example": {netip.MustParseAddr("8.8.4.4"), netip.MustParseAddr("127.0.0.1")}}
	if _, err := ResolveApprovedTarget(context.Background(), "https://mixed.example/blob", false, false, true, resolver); err == nil {
		t.Fatal("mixed public/private DNS result accepted")
	}
	if _, err := ResolveApprovedTarget(context.Background(), "https://127.0.0.1/blob", false, false, true, resolver); err == nil {
		t.Fatal("loopback target accepted")
	}
	if _, err := ResolveApprovedTarget(context.Background(), "https://user:pass@cdn.example/blob", false, false, true, resolver); err == nil {
		t.Fatal("userinfo target accepted")
	}
}
