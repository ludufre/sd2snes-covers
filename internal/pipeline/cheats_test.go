package pipeline

import "testing"

// TestCheatSinkClaim verifies the run-wide name-claim logic that distinguishes a
// duplicate ROM (same CRC) from a real collision (different game, same filename),
// and that release frees a name for reuse.
func TestCheatSinkClaim(t *testing.T) {
	s := &cheatSink{claimed: map[string]string{}}

	if busy, _ := s.tryClaim("smw.yml", "AAAA"); busy {
		t.Fatal("first claim of a free name should not be busy")
	}

	// Same name, same CRC -> a duplicate ROM, not a collision.
	if busy, same := s.tryClaim("smw.yml", "AAAA"); !busy || !same {
		t.Errorf("same CRC: busy=%v sameGame=%v, want true,true", busy, same)
	}

	// Same name, different CRC -> a real collision.
	if busy, same := s.tryClaim("smw.yml", "BBBB"); !busy || same {
		t.Errorf("different CRC: busy=%v sameGame=%v, want true,false", busy, same)
	}

	// After releasing, the name is claimable again (e.g. the first ROM had no cheat).
	s.release("smw.yml")
	if busy, _ := s.tryClaim("smw.yml", "CCCC"); busy {
		t.Error("after release, claiming the name should succeed")
	}
}
