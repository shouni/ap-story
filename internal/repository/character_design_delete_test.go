package repository

import (
	"context"
	"strings"
	"testing"
)

func putCharacterDesignImage(store *memStore, characterID, jobID string) string {
	path := "gs://test-bucket/character/" + characterID + "/" + jobID + ".png"
	store.files[path] = []byte("binary")
	return path
}

func TestDeleteCharacterDesignRemovesImageAndDesignJobState(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	imagePath := putCharacterDesignImage(store, "zundamon", "c20260718-000000-eeee5555")
	// 現行形式: 単体生成ジョブの state は design-jobs/ 配下。ジョブごと削除される。
	statePath := "gs://test-bucket/design-jobs/c20260718-000000-eeee5555/comic_state.json"
	store.files[statePath] = []byte(
		`{"version":1,"id":"c20260718-000000-eeee5555","design_sheets":[{"character_id":"zundamon","image_url":"` + imagePath + `"}]}`)

	repo := newTestRepository(store)
	if err := repo.DeleteCharacterDesign(context.Background(), "zundamon", "c20260718-000000-eeee5555"); err != nil {
		t.Fatalf("DeleteCharacterDesign failed: %v", err)
	}

	if _, ok := store.files[imagePath]; ok {
		t.Error("design sheet image was not deleted")
	}
	if _, ok := store.files[statePath]; ok {
		t.Error("design job state was not deleted")
	}
}

func TestDeleteCharacterDesignRemovesImageAndLegacyDesignOnlyState(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	imagePath := putCharacterDesignImage(store, "zundamon", "c20260718-000000-aaaa1111")
	// 旧形式: 単体生成ジョブの state が comics/ に残っている（章立てなしで判別して削除）。
	putState(store, "c20260718-000000-aaaa1111",
		`{"version":1,"id":"c20260718-000000-aaaa1111","design_sheets":[{"character_id":"zundamon","image_url":"`+imagePath+`"}]}`)

	repo := newTestRepository(store)
	if err := repo.DeleteCharacterDesign(context.Background(), "zundamon", "c20260718-000000-aaaa1111"); err != nil {
		t.Fatalf("DeleteCharacterDesign failed: %v", err)
	}

	if _, ok := store.files[imagePath]; ok {
		t.Error("design sheet image was not deleted")
	}
	if _, ok := store.files["gs://test-bucket/comics/c20260718-000000-aaaa1111/comic_state.json"]; ok {
		t.Error("legacy design-only job state was not deleted")
	}
}

func TestDeleteCharacterDesignKeepsComicStateButPrunesReference(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	imagePath := putCharacterDesignImage(store, "zundamon", "c20260718-000000-bbbb2222")
	// 章立てあり = 作品ジョブ。state は残し、削除した画像への参照だけを取り除く。
	putState(store, "c20260718-000000-bbbb2222",
		`{"version":1,"id":"c20260718-000000-bbbb2222","chapters":[{"id":"ch01"}],`+
			`"design_sheets":[{"character_id":"zundamon","image_url":"`+imagePath+`"},`+
			`{"character_id":"metan","image_url":"gs://test-bucket/character/metan/other.png"}]}`)

	repo := newTestRepository(store)
	if err := repo.DeleteCharacterDesign(context.Background(), "zundamon", "c20260718-000000-bbbb2222"); err != nil {
		t.Fatalf("DeleteCharacterDesign failed: %v", err)
	}

	if _, ok := store.files[imagePath]; ok {
		t.Error("design sheet image was not deleted")
	}
	stateJSON, ok := store.files["gs://test-bucket/comics/c20260718-000000-bbbb2222/comic_state.json"]
	if !ok {
		t.Fatal("comic job state must not be deleted")
	}
	if strings.Contains(string(stateJSON), imagePath) {
		t.Error("deleted image reference remains in state")
	}
	if !strings.Contains(string(stateJSON), "metan/other.png") {
		t.Error("unrelated design sheet reference was lost")
	}
}

func TestDeleteCharacterDesignWithoutStateRemovesImageOnly(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	imagePath := putCharacterDesignImage(store, "zundamon", "c20260718-000000-cccc3333")

	repo := newTestRepository(store)
	if err := repo.DeleteCharacterDesign(context.Background(), "zundamon", "c20260718-000000-cccc3333"); err != nil {
		t.Fatalf("DeleteCharacterDesign failed: %v", err)
	}
	if _, ok := store.files[imagePath]; ok {
		t.Error("design sheet image was not deleted")
	}
}

func TestDeleteCharacterDesignFailsWhenImageMissing(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(newMemStore())
	if err := repo.DeleteCharacterDesign(context.Background(), "zundamon", "c20260718-000000-dddd4444"); err == nil {
		t.Error("DeleteCharacterDesign for missing image succeeded, want error")
	}
}

func TestDeleteCharacterDesignRejectsInvalidJobID(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(newMemStore())
	if err := repo.DeleteCharacterDesign(context.Background(), "zundamon", "../escape"); err == nil {
		t.Error("DeleteCharacterDesign with invalid job id succeeded, want error")
	}
}
