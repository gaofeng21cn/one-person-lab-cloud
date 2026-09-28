package identity

import "testing"

func TestRepositorySlugCandidate(t *testing.T) {
	cases := []struct{ email, want string }{
		{"huangrende@fenggaolab.org", "huangrende"},
		{"Alice.Brown+ibd@example.com", "alice.brown-ibd"},
		{"user_name@example.com", "user_name"},
		{"  Trim.Me  @x.org", "trim.me"},
		{"@example.com", "tenant"},
		{"", "tenant"},
		{"a@b@c", "a-b"},
		{"Ünïcode@example.com", "n-code"},
		{"1234567890@example.com", "1234567890"},
	}
	for _, tc := range cases {
		if got := repositorySlugCandidate(tc.email); got != tc.want {
			t.Errorf("repositorySlugCandidate(%q)=%q want %q", tc.email, got, tc.want)
		}
	}
	if !repositoryNamePattern.MatchString(repositorySlugCandidate("huangrende@fenggaolab.org")) {
		t.Fatal("candidate must be a valid repository name")
	}
}

func TestLegacyRepositorySlug(t *testing.T) {
	if got, want := legacyRepositorySlug("tenant-existing"), "tenant-"+hash("tenant-existing")[:24]; got != want {
		t.Fatalf("legacyRepositorySlug = %q, want %q", got, want)
	}
	if !repositoryNamePattern.MatchString(legacyRepositorySlug("tenant-existing")) {
		t.Fatal("legacy candidate must be a valid repository name")
	}
}
