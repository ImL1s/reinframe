package adapter

import (
	"strings"
	"testing"
)

func TestGrokACPHonestyNotesDoNotClaimDispositionGO(t *testing.T) {
	t.Parallel()
	pre := NewGrokACPFoundationManifest()
	if honestyClaimsDispositionGO(pre.HonestyNote) {
		t.Fatalf("pre-handshake honesty_note must not claim disposition GO: %q", pre.HonestyNote)
	}
	caps := GrokACPNegotiatedCaps{
		ProtocolVersion: 1,
		LoadSession:     true,
		AuthMethods:     []string{"cached_token"},
	}
	post := ManifestFromNegotiated(caps)
	if honestyClaimsDispositionGO(post.HonestyNote) {
		t.Fatalf("post-handshake honesty_note must not claim disposition GO: %q", post.HonestyNote)
	}
}

// honestyClaimsDispositionGO detects historical template tokens that over-claim GO.
func honestyClaimsDispositionGO(s string) bool {
	for _, bad := range []string{
		"evidence GO",
		"GO via cmd/groklive",
		"GO on harness",
	} {
		if strings.Contains(s, bad) {
			return true
		}
	}
	return false
}
