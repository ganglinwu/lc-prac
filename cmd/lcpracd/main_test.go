package main

import "testing"

func TestParseTokens(t *testing.T) {
	got, err := parseTokens("ganglin:abc123, guest:def456")
	if err != nil {
		t.Fatalf("parseTokens: %v", err)
	}
	if got["abc123"] != "ganglin" || got["def456"] != "guest" {
		t.Fatalf("got %v", got)
	}
}

func TestParseTokensRejectsBad(t *testing.T) {
	for _, in := range []string{"", "   ", "nocolon", "ganglin:", ":abc123", ","} {
		if _, err := parseTokens(in); err == nil {
			t.Errorf("parseTokens(%q): want error, got nil", in)
		}
	}
}
