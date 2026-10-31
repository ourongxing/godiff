package git

import (
	"net/url"
	"strings"
)

// WebURL returns a remote's browser address, or "" for a local path or an
// unsupported transport. SSH and Git transports use their HTTPS equivalent.
func (r Remote) WebURL() string {
	raw := strings.TrimSpace(r.URL)
	if !strings.Contains(raw, "://") {
		// Git's scp-like form: [user@]host:owner/repository.git.
		host, path, ok := strings.Cut(raw, ":")
		if !ok || len(host) <= 1 || strings.ContainsAny(host, `/\`) || strings.Contains(path, `\`) {
			return ""
		}
		raw = "ssh://" + host + "/" + path
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
	case "ssh", "git", "git+ssh":
		u.Scheme = "https"
		if port := u.Port(); port != "" {
			u.Host = strings.TrimSuffix(u.Host, ":"+port)
		}
	default:
		return ""
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), ".git")
	if strings.Trim(u.Path, "/") == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.RawPath = nil, "", "", ""
	return u.String()
}

// WebURL prefers this clone's origin, then upstream, then another web remote.
func (r *Repo) WebURL() string {
	remotes := r.Remotes()
	for _, name := range []string{"origin", "upstream", ""} {
		for _, remote := range remotes {
			if name == "" || remote.Name == name {
				if address := remote.WebURL(); address != "" {
					return address
				}
			}
		}
	}
	return ""
}
