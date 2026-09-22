package domain

import "testing"

func TestCompareBuildsTextProtocol(t *testing.T) {
	from := Source{ID: "old", ActCode: "ORDER-270", Revision: "1", ContentSHA256: "a", SizeBytes: 8, Content: []byte("one\ntwo\n")}
	to := Source{ID: "new", ActCode: "ORDER-270", Revision: "2", ContentSHA256: "b", SizeBytes: 10, Content: []byte("two\nthree\n")}
	diff := Compare(from, to)
	if !diff.Changed || diff.TextChanges == nil || len(diff.TextChanges.Added) != 1 || diff.TextChanges.Added[0] != "three" || len(diff.TextChanges.Removed) != 1 || diff.TextChanges.Removed[0] != "one" {
		t.Fatalf("unexpected diff: %#v", diff)
	}
}

func TestCompareBinaryKeepsHashProtocol(t *testing.T) {
	diff := Compare(Source{ContentSHA256: "a", Content: []byte{0, 1}}, Source{ContentSHA256: "b", Content: []byte{0, 2}})
	if !diff.Changed || diff.TextChanges != nil {
		t.Fatalf("unexpected binary diff: %#v", diff)
	}
}
