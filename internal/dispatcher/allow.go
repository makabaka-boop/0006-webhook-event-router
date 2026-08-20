package dispatcher

import (
	"net"
	"net/url"
)

// Allowed 判断目标 URL 是否允许转发。返回 false 表示被白名单拒绝。
func Allowed(rawurl string, allowPrivate, allowLoopback bool) bool {
	u, err := url.Parse(rawurl)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Hostname() == "" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return false
		}
		ip = ips[0]
	}
	if ip.IsLoopback() && !allowLoopback {
		return false
	}
	if ip.IsPrivate() && !allowPrivate {
		return false
	}
	return true
}
