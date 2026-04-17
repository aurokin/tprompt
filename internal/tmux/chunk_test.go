package tmux

import (
	"strings"
	"testing"
)

func TestChunkByRunesEmpty(t *testing.T) {
	if got := chunkByRunes("", 4096); got != nil {
		t.Fatalf("want nil for empty input, got %v", got)
	}
}

func TestChunkByRunesSingleChunk(t *testing.T) {
	s := "hello world"
	got := chunkByRunes(s, 4096)
	if len(got) != 1 || got[0] != s {
		t.Fatalf("want single chunk, got %v", got)
	}
}

func TestChunkByRunesExactBoundary(t *testing.T) {
	s := strings.Repeat("a", 4096)
	got := chunkByRunes(s, 4096)
	if len(got) != 1 || got[0] != s {
		t.Fatalf("want single chunk when len == max, got %d chunks", len(got))
	}
}

func TestChunkByRunesASCII(t *testing.T) {
	s := strings.Repeat("a", 9000)
	got := chunkByRunes(s, 4096)
	if len(got) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(got))
	}
	if len(got[0]) != 4096 || len(got[1]) != 4096 || len(got[2]) != 808 {
		t.Fatalf("unexpected chunk sizes: %d %d %d", len(got[0]), len(got[1]), len(got[2]))
	}
	if joined := strings.Join(got, ""); joined != s {
		t.Fatal("joined chunks do not equal input")
	}
}

func TestChunkByRunesMultiByteSplit(t *testing.T) {
	// 4-byte rune U+1F600 "😀". Repeat so a chunk boundary falls mid-rune.
	rune4 := "\xF0\x9F\x98\x80"
	s := strings.Repeat(rune4, 2000) // 8000 bytes, exact multiple of 4

	got := chunkByRunes(s, 4096)
	if len(got) < 2 {
		t.Fatalf("want multiple chunks, got %d", len(got))
	}
	// Each chunk length must be a multiple of 4 so no rune is split.
	for i, c := range got {
		if len(c)%4 != 0 {
			t.Fatalf("chunk %d has length %d (not multiple of 4) — rune split", i, len(c))
		}
	}
	if joined := strings.Join(got, ""); joined != s {
		t.Fatal("joined chunks do not equal input")
	}
}

func TestChunkByRunesAllChunksUnderCap(t *testing.T) {
	rune4 := "\xF0\x9F\x98\x80"
	s := strings.Repeat(rune4, 3000)
	got := chunkByRunes(s, 100)
	for i, c := range got {
		if len(c) > 100 {
			t.Fatalf("chunk %d length %d exceeds cap", i, len(c))
		}
	}
}
