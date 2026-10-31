package git

import "testing"

func TestRemoteWebURL(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:acme/app.git":                                      "https://github.com/acme/app",
		"https://github.com/acme/app.git/":                                 "https://github.com/acme/app",
		"ssh://git@gitlab.com:2222/team/subgroup/app.git":                  "https://gitlab.com/team/subgroup/app",
		"git://gitee.com/acme/app.git":                                     "https://gitee.com/acme/app",
		"https://user:secret@example.com/acme/app.git?token=secret#branch": "https://example.com/acme/app",
		"http://git.example.com:8080/acme/app.git":                         "http://git.example.com:8080/acme/app",
		"/tmp/local-repo.git":                                              "",
		"../local-repo":                                                    "",
		`C:\repos\app.git`:                                                 "",
		"C:/repos/app.git":                                                 "",
		"file:///tmp/local-repo.git":                                       "",
		"https://github.com":                                               "",
		"https://github.com/%invalid":                                      "",
	} {
		if got := (Remote{URL: remote}).WebURL(); got != want {
			t.Errorf("%s: %q, want %q", remote, got, want)
		}
	}
}

func TestRepositoryWebURL(t *testing.T) {
	dir, repo := newRepo(t)
	if got := repo.WebURL(); got != "" {
		t.Fatalf("a repository without remotes returned %q", got)
	}
	gitIn(t, dir, "remote", "add", "upstream", "git@github.com:upstream/app.git")
	gitIn(t, dir, "remote", "add", "origin", "git@gitlab.com:mine/app.git")
	if got := repo.WebURL(); got != "https://gitlab.com/mine/app" {
		t.Fatalf("origin should take priority: %s", got)
	}
	gitIn(t, dir, "remote", "set-url", "origin", "../local-repo")
	if got := repo.WebURL(); got != "https://github.com/upstream/app" {
		t.Fatalf("local origin should fall back to upstream: %s", got)
	}
}
