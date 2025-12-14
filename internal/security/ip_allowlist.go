package security

import (
	"net"
	"net/http"
	"strings"
)

func ParseCIDRAllowlist(cidrs []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func IPAllowlist(allow []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allow) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				WriteJSONError(w, r, http.StatusForbidden, "forbidden")
				return
			}

			ip := net.ParseIP(host)
			if ip == nil {
				WriteJSONError(w, r, http.StatusForbidden, "forbidden")
				return
			}

			for _, n := range allow {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}

			WriteJSONError(w, r, http.StatusForbidden, "forbidden")
		})
	}
}
