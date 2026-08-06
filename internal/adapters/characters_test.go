package adapters

import (
	"context"
	"testing"
)

func TestLoadCharactersFallsBackToDefaultWhenPathEmpty(t *testing.T) {
	t.Parallel()

	characters, err := LoadCharacters(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("LoadCharacters with empty path failed: %v", err)
	}
	if characters == nil || len(characters.List) == 0 {
		t.Fatal("LoadCharacters with empty path returned no characters, want embedded defaults")
	}
	if characters.GetCharacter("zundamon") == nil {
		t.Error("expected embedded default characters to include zundamon")
	}
}
