package git

import (
	"net/url"
	"strings"
)

// WebURL returns a remote's browser address, or "" for a local path or an
// unsupported transport. SSH and Git transports use their HTTPS equivalent.
// Known hosting services link directly to branch; others keep the repository URL.
func (r Remote) WebURL(branch string) string {
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
	if branch != "" {
		var tree string
		switch host := strings.ToLower(u.Hostname()); {
		case host == "github.com", host == "gitee.com":
			tree = "/tree/"
		case host == "gitlab.com", strings.HasPrefix(host, "gitlab."):
			tree = "/-/tree/"
		}
		if tree != "" {
			return u.String() + tree + url.PathEscape(branch)
		}
	}
	return u.String()
}

// WebURL opens the branch's tracked remote ref when configured. Otherwise it
// uses the local branch name with origin, upstream, or another web remote.
func (r *Repo) WebURL(branch string) string {
	remotes := r.Remotes()
	if len(remotes) == 0 {
		return ""
	}
	if branch != "" {
		tracking := r.gitString("for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+branch)
		name, ref, ok := strings.Cut(tracking, "\x00")
		if ok && strings.HasPrefix(ref, "refs/heads/") {
			for _, remote := range remotes {
				if remote.Name == name {
					if address := remote.WebURL(strings.TrimPrefix(ref, "refs/heads/")); address != "" {
						return address
					}
				}
			}
		}
	}
	for _, name := range []string{"origin", "upstream", ""} {
		for _, remote := range remotes {
			if name == "" || remote.Name == name {
				if address := remote.WebURL(branch); address != "" {
					return address
				}
			}
		}
	}
	return ""
}
