// Package security provides URL, address, redirect and policy validation.
package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "100:0:0:1::/64",
	"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "ff00::/8",
)

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		out = append(out, netip.MustParsePrefix(value))
	}
	return out
}

func IsBlockedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ApprovedTarget struct {
	URL       *url.URL
	IP        netip.Addr
	Host      string
	Port      string
	Address   string
	Authority string
}

// ResolveApprovedTarget resolves and validates a dynamic redirect/token
// target, then returns one approved numeric address for Managed Upstream Nginx to connect to.
// The original hostname is retained for Host and TLS SNI verification.
func ResolveApprovedTarget(ctx context.Context, rawURL string, allowHTTP, allowPrivate, rejectMixed bool, resolver Resolver) (ApprovedTarget, error) {
	targets, err := ResolveApprovedTargets(ctx, rawURL, allowHTTP, allowPrivate, rejectMixed, resolver)
	if err != nil {
		return ApprovedTarget{}, err
	}
	return targets[0], nil
}

// ResolveApprovedTargets returns every unique approved address in stable order.
// Callers can provide connection failover without allowing a second DNS lookup
// to bypass the policy decision.
func ResolveApprovedTargets(ctx context.Context, rawURL string, allowHTTP, allowPrivate, rejectMixed bool, resolver Resolver) ([]ApprovedTarget, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !u.IsAbs() || u.Hostname() == "" || u.Opaque != "" {
		return nil, errors.New("target must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return nil, errors.New("target protocol is not allowed")
	}
	if u.User != nil {
		return nil, errors.New("target credentials are not allowed")
	}
	if strings.ContainsAny(u.Host, "\x00\r\n") {
		return nil, errors.New("target host contains control characters")
	}
	u.Fragment = ""
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return nil, errors.New("target port is invalid")
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	var addresses []netip.Addr
	if ip, parseErr := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); parseErr == nil {
		addresses = []netip.Addr{ip.Unmap()}
	} else {
		addresses, err = resolver.LookupNetIP(ctx, "ip", u.Hostname())
		if err != nil {
			return nil, fmt.Errorf("resolve target: %w", err)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("target hostname returned no addresses")
	}
	approved := make([]netip.Addr, 0, len(addresses))
	blocked := false
	seen := make(map[netip.Addr]bool)
	for _, address := range addresses {
		address = address.Unmap()
		if IsBlockedIP(address) && !allowPrivate {
			blocked = true
			continue
		}
		if !seen[address] {
			seen[address] = true
			approved = append(approved, address)
		}
	}
	if blocked && rejectMixed {
		return nil, errors.New("target hostname returned mixed permitted and blocked addresses")
	}
	if len(approved) == 0 {
		return nil, errors.New("target hostname returned no permitted address")
	}
	sort.Slice(approved, func(i, j int) bool { return approved[i].Less(approved[j]) })
	targets := make([]ApprovedTarget, 0, len(approved))
	for _, selected := range approved {
		targets = append(targets, ApprovedTarget{URL: u, IP: selected, Host: u.Hostname(), Port: port,
			Address: net.JoinHostPort(selected.String(), port), Authority: u.Host})
	}
	return targets, nil
}

func ValidateResolvedURL(ctx context.Context, rawURL string, allowHTTP, allowPrivate bool, resolver Resolver) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return errors.New("invalid upstream URL")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return errors.New("upstream protocol is not allowed")
	}
	if u.User != nil {
		return errors.New("upstream credentials are not allowed")
	}
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil {
		if !allowPrivate && IsBlockedIP(ip) {
			return fmt.Errorf("upstream address %s is blocked", ip)
		}
		return nil
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil {
		return fmt.Errorf("resolve upstream: %w", err)
	}
	if len(addrs) == 0 {
		return errors.New("upstream hostname returned no addresses")
	}
	if !allowPrivate {
		for _, ip := range addrs {
			if IsBlockedIP(ip) {
				return fmt.Errorf("upstream resolved to blocked address %s", ip)
			}
		}
	}
	return nil
}

// ParseOriginURL validates and canonicalizes an HTTP(S) origin. Cluster node
// addresses deliberately do not accept credentials, path prefixes, queries or
// fragments so protocol endpoints can be resolved without string
// concatenation or ambiguous URL semantics.
func ParseOriginURL(rawURL string, allowHTTP bool) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Hostname() == "" {
		return nil, errors.New("invalid origin URL")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return nil, errors.New("origin protocol is not allowed")
	}
	if u.User != nil {
		return nil, errors.New("origin credentials are not allowed")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("origin query and fragment are not allowed")
	}
	if escaped := u.EscapedPath(); escaped != "" && escaped != "/" {
		return nil, errors.New("origin path prefix is not allowed")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, errors.New("origin port is invalid")
		}
	}
	u.Path = ""
	u.RawPath = ""
	return u, nil
}

// ResolveOriginEndpoint resolves an absolute protocol path against a validated
// origin. The returned URL never inherits a user-supplied path, query or
// fragment from the origin.
func ResolveOriginEndpoint(rawOrigin, endpoint string, allowHTTP bool) (string, error) {
	origin, err := ParseOriginURL(rawOrigin, allowHTTP)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(endpoint, "/") {
		return "", errors.New("origin endpoint must be an absolute path")
	}
	reference := &url.URL{Path: endpoint}
	return origin.ResolveReference(reference).String(), nil
}

// ValidateOutboundURLSyntax applies the structural portion of the outbound
// request policy without performing DNS resolution. It is suitable for strict
// configuration loading; callers must still use ValidateResolvedURL and a
// SafeDialer immediately before sending a request.
func ValidateOutboundURLSyntax(rawURL string, allowHTTP bool) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Hostname() == "" {
		return errors.New("invalid outbound URL")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return errors.New("outbound protocol is not allowed")
	}
	if u.User != nil {
		return errors.New("outbound credentials are not allowed")
	}
	if u.Fragment != "" {
		return errors.New("outbound URL fragment is not allowed")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errors.New("outbound port is invalid")
		}
	}
	return nil
}

// SafeDialer resolves on every new connection and validates every returned IP
// before dialing it. That closes the DNS-rebinding gap left by save-time checks.
type SafeDialer struct {
	Resolver     Resolver
	Dialer       net.Dialer
	AllowPrivate bool
}

func (d *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	resolver := d.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	var addrs []netip.Addr
	if ip, parseErr := netip.ParseAddr(strings.Trim(host, "[]")); parseErr == nil {
		addrs = []netip.Addr{ip}
	} else {
		addrs, err = resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("hostname returned no addresses")
	}
	var failures []string
	for _, ip := range addrs {
		if !d.AllowPrivate && IsBlockedIP(ip) {
			failures = append(failures, ip.String()+" blocked")
			continue
		}
		target := net.JoinHostPort(ip.String(), port)
		conn, dialErr := d.Dialer.DialContext(ctx, network, target)
		if dialErr == nil {
			return conn, nil
		}
		failures = append(failures, dialErr.Error())
	}
	return nil, fmt.Errorf("no safe upstream address available: %s", strings.Join(failures, "; "))
}

func NewSafeDialer(timeout, keepAlive time.Duration, allowPrivate bool) *SafeDialer {
	return &SafeDialer{Resolver: net.DefaultResolver, Dialer: net.Dialer{Timeout: timeout, KeepAlive: keepAlive}, AllowPrivate: allowPrivate}
}

func ClientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

// RequestClientIP accepts X-Real-IP only from an explicitly trusted TCP peer or
// from the permission-controlled frontend Unix socket. Untrusted peers are
// always identified by RemoteAddr, regardless of supplied forwarding headers.
func RequestClientIP(request *http.Request, trustedProxies CIDRList, trustUnixSocket bool) string {
	if request == nil {
		return ""
	}
	peer := ClientIP(request.RemoteAddr)
	trustedPeer := trustedProxies.Contains(peer)
	if trustUnixSocket && net.ParseIP(peer) == nil {
		trustedPeer = true
	}
	if trustedPeer {
		if value := strings.TrimSpace(request.Header.Get("X-Real-IP")); net.ParseIP(value) != nil {
			return value
		}
	}
	return peer
}

// ValidRequestAuthority accepts only a plain host or host:port authority that
// can safely be used to construct an HTTPS public origin.
func ValidRequestAuthority(value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\r\n/\\ {};$\"") {
		return false
	}
	parsed, err := url.Parse("https://" + value)
	if err != nil || parsed.Host != value || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return false
		}
	}
	return true
}

type CIDRList []*net.IPNet

func ParseCIDRs(values []string) (CIDRList, error) {
	out := make(CIDRList, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		out = append(out, network)
	}
	return out, nil
}

func (l CIDRList) Allows(ip string) bool {
	if len(l) == 0 {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, network := range l {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// Contains differs from Allows by treating an empty list as trusting no peers.
func (l CIDRList) Contains(ip string) bool {
	if len(l) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, network := range l {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}
