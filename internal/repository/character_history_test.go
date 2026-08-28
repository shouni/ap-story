package repository

import (
	"context"
	"testing"
)

func TestListCharacterDesignHistoryReturnsNewestFirst(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.put("gs://test-bucket/character/zundamon/job-1.png", "a")
	store.put("gs://test-bucket/character/zundamon/job-2.png", "b")
	// 別キャラクター（混ざらないことの確認用）
	store.put("gs://test-bucket/character/metan/job-9.png", "c")
	// 合成生成のタグ違いディレクトリ（対象外であることの確認用）
	store.put("gs://test-bucket/character/zundamon_metan/job-3.png", "d")

	repo := newTestRepository(store)
	items, err := repo.ListCharacterDesignHistory(context.Background(), "zundamon")
	if err != nil {
		t.Fatalf("ListCharacterDesignHistory failed: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2 entries", items)
	}
	if items[0].JobID != "job-2" || items[1].JobID != "job-1" {
		t.Errorf("items = %+v, want [job-2, job-1] (newest first)", items)
	}
	if items[0].ImageURL != "gs://test-bucket/character/zundamon/job-2.png" {
		t.Errorf("ImageURL = %q, want the raw gs:// URI", items[0].ImageURL)
	}
}

func TestListCharacterDesignHistoryReturnsEmptyForUnknownCharacter(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	repo := newTestRepository(store)

	items, err := repo.ListCharacterDesignHistory(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("ListCharacterDesignHistory failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want empty", items)
	}
}
