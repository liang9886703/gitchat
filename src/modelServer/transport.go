package modelServer

import (
	"context"
	"errors"
	"gitchat/src/config"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NormalizeEndpoint accepts API roots or pasted operation URLs without accepting
// credentials in URLs. Local/private services are available only on local installs.
func NormalizeEndpoint(raw, protocol string, allowLocal bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("请填写不含密钥、查询参数的 HTTP(S) API 地址")
	}
	if u.Scheme == "http" && !allowLocal {
		return "", errors.New("远程部署必须使用 HTTPS 模型服务")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !allowedIP(ip, allowLocal) {
		return "", errors.New("此部署不允许访问本机或内网模型地址")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	suffixes := []string{"/chat/completions", "/responses", "/messages", "/api/chat", "/api/tags"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(u.Path, suffix) {
			u.Path = strings.TrimSuffix(u.Path, suffix)
			break
		}
	}
	if protocol == "gemini" {
		if i := strings.Index(u.Path, "/models/"); i >= 0 {
			u.Path = u.Path[:i]
		}
	}
	if u.Path == "" {
		switch protocol {
		case "ollama":
		case "gemini":
			u.Path = "/v1beta"
		default:
			u.Path = "/v1"
		}
	}
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}
func allowedIP(ip net.IP, allowLocal bool) bool {
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return allowLocal
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "2001:db8::/32"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return ip.IsGlobalUnicast()
}
func NewManagedProvider(cfg *config.Config, allowLocal bool) (Provider, error) {
	instance, err := NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	p := instance.(*provider)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !allowedIP(ip.IP, allowLocal) {
				return nil, errors.New("model address is not allowed")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			err = e
		}
		if err == nil {
			err = errors.New("no model address found")
		}
		return nil, err
	}
	p.client.Transport = transport
	return p, nil
}
