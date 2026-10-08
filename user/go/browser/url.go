package main

import (
	"strings"

	"virelai/vi"
	"virelai/webrender"
)

const maxURLBytes = 2048
const maxGetURLBytes = 4096

func browserArgs(args []string) ([]string, string, bool) {
	if len(args) == 0 {
		return args, "", true
	}
	out, dns := []string{args[0]}, ""
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "--dns=") {
			dns = strings.TrimPrefix(arg, "--dns=")
			ip, ok := webrender.ParseIPv4(dns)
			if !ok || ip == ([4]byte{}) {
				return nil, "", false
			}
		} else {
			out = append(out, arg)
		}
	}
	return out, dns, true
}

// parseBrowserURL adds input validation around the existing URL codec. No
// userinfo, IPv6, control bytes or request-line/header injection is accepted.
func parseBrowserURL(raw string) (webrender.URL, bool) {
	if len(raw) > maxGetURLBytes || strings.ContainsAny(raw, "\x00\r\n\t ") {
		return webrender.URL{}, false
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	prefix := 7
	if webrender.IsHTTPSURL(raw) {
		prefix = 8
	} else if !webrender.IsHTTPURL(raw) {
		return webrender.URL{}, false
	}
	rest := raw[prefix:]
	if q := strings.IndexByte(rest, '?'); q >= 0 {
		if slash := strings.IndexByte(rest, '/'); slash < 0 || q < slash {
			raw = raw[:prefix+q] + "/" + raw[prefix+q:]
		}
	}
	u, ok := webrender.ParseURL(raw)
	if !ok || u.Port == 0 || u.Path == "" || u.Path[0] != '/' || !validHost(u.Host) {
		return webrender.URL{}, false
	}
	u.Host = strings.ToLower(u.Host)
	if u.IsIP {
		u.Host = vi.FormatIPv4(u.IPv4)
	}
	for i := 0; i < len(u.Path); i++ {
		if u.Path[i] < 0x21 || u.Path[i] >= 0x7f {
			return webrender.URL{}, false
		}
	}
	return u, true
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func urlAuthority(u webrender.URL) string {
	host := u.Host
	def := uint16(80)
	if u.Scheme == "https" {
		def = 443
	}
	if u.Port != def {
		host += ":" + itoa(int(u.Port))
	}
	return host
}

func resolveReference(base, ref string) string {
	if i := strings.IndexByte(ref, ':'); i > 0 && isSchemeAlpha(ref[:i]) {
		return ref // unsupported schemes must reach the named refusal
	}
	u, network := parseBrowserURL(base)
	fragment := ""
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		fragment, ref = ref[i:], ref[:i]
	}
	cleanBase := strings.SplitN(base, "#", 2)[0]
	if network {
		origin := u.Scheme + "://" + urlAuthority(u)
		if strings.HasPrefix(ref, "//") {
			return u.Scheme + ":" + ref + fragment
		}
		path := strings.SplitN(u.Path, "?", 2)[0]
		switch {
		case ref == "":
			return cleanBase + fragment
		case ref[0] == '?':
			return origin + path + ref + fragment
		default:
			resolved, ok := webrender.ResolveHref(path, ref)
			if ok {
				return origin + resolved + fragment
			}
			return ref + fragment
		}
	}
	if ref == "" {
		return cleanBase + fragment
	}
	if resolved, ok := webrender.ResolveHref(cleanBase, ref); ok {
		return resolved + fragment
	}
	return ref + fragment
}

func isPageRedirect(code int) bool {
	return code == 301 || code == 302 || code == 303 || code == 307 || code == 308
}

func redirectTarget(from webrender.URL, location string) (webrender.URL, string) {
	if location == "" {
		return webrender.URL{}, "redirect-location"
	}
	next, ok := parseBrowserURL(resolveReference(formatNavURL(from), strings.TrimSpace(location)))
	if !ok {
		return webrender.URL{}, "redirect-location"
	}
	if from.Scheme == "https" && next.Scheme != "https" {
		return webrender.URL{}, "redirect-downgrade"
	}
	return next, ""
}
