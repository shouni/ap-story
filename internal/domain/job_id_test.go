package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/shouni/go-utils/jobid"
)

// legacyJobID は v1.5.0 以前に採番していた形式の ID です（2026-07-17 11:30:45 UTC）。
// 過去のジョブは GCS の成果物パスにこの形式で残り続けます。
const legacyJobID = "c20260717-113045-1a2b3c4d"

func TestNewJobIDFormat(t *testing.T) {
	t.Parallel()

	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}
	if err := ValidateJobID(id); err != nil {
		t.Errorf("NewJobID produced invalid ID %q: %v", id, err)
	}
	if !strings.HasPrefix(id, "c") {
		t.Errorf("id = %q, want prefix 'c'", id)
	}

	other, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}
	if id == other {
		t.Errorf("two NewJobID calls returned the same ID %q", id)
	}
}

// TestNewJobIDCarriesCreatedAt は、採番した ID から生成時刻を復元できることを確認します。
// 履歴一覧の日時表示と並び順はこの復元に依存しているため、採番形式を変えると
// jobid.CreatedAt が読めなくなり、一覧の日時が無言で空欄になります。
func TestNewJobIDCarriesCreatedAt(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Truncate(time.Second)
	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}
	after := time.Now().UTC()

	createdAt, err := jobid.CreatedAt(id)
	if err != nil {
		t.Fatalf("jobid.CreatedAt(%q) failed: %v", id, err)
	}
	if createdAt.Before(before) || createdAt.After(after) {
		t.Errorf("jobid.CreatedAt(%q) = %v, want between %v and %v", id, createdAt, before, after)
	}
}

// TestNewJobIDSortsAgainstLegacyIDs は、新形式で採番した ID が旧形式の ID と混在しても
// 作成日時の降順に並ぶことを確認します。
//
// 採番形式を jobid.New へ寄せたことで、辞書順では新旧が分離します（"c-" < "c2" のため、
// 新形式の ID が常に旧形式より後ろに回る）。履歴の並び順は paging.WithSortKey(jobid.SortKey)
// に依存しており、既定の ID 降順へ戻すと一覧はエラーにならず静かに並び替わります。
func TestNewJobIDSortsAgainstLegacyIDs(t *testing.T) {
	t.Parallel()

	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}

	key, legacyKey := jobid.SortKey(id), jobid.SortKey(legacyJobID)
	if key == "" {
		t.Fatalf("jobid.SortKey(%q) が空です。採番形式から生成時刻を復元できません", id)
	}
	if legacyKey == "" {
		t.Fatalf("jobid.SortKey(%q) が空です。旧形式の ID が読めなくなっています", legacyJobID)
	}
	// 新しく採番した ID の方が必ず後の時刻になる。
	if key <= legacyKey {
		t.Errorf("SortKey(%q) = %q, want > SortKey(%q) = %q", id, key, legacyJobID, legacyKey)
	}
}

func TestValidateJobID(t *testing.T) {
	t.Parallel()

	// アンダースコアは go-utils/jobid への統一で許容されるようになりました
	// （ap-mv 由来の規則に揃えたため）。URL・GCS パスのいずれでも安全な文字です。
	valid := []string{"c20260717-113045-1a2b3c4d", "abc", "ABC-123", "a_b"}
	for _, id := range valid {
		if err := ValidateJobID(id); err != nil {
			t.Errorf("ValidateJobID(%q) = %v, want nil", id, err)
		}
	}

	// 先頭のハイフン・アンダースコアと 128 文字超も、統一規則で新たに拒否されます。
	invalid := []string{"", "a/b", "../etc", "a b", "日本語", "a.b",
		"-leading", "_leading", strings.Repeat("a", 129)}
	for _, id := range invalid {
		if err := ValidateJobID(id); err == nil {
			t.Errorf("ValidateJobID(%q) = nil, want error", id)
		}
	}
}

func TestSanitizeJobID(t *testing.T) {
	t.Parallel()

	// パス風の値は Base で正規化される
	got, err := SanitizeJobID("comics/c123-abc")
	if err != nil || got != "c123-abc" {
		t.Errorf("SanitizeJobID = %q, %v; want c123-abc", got, err)
	}

	// トラバーサルは正規化後の検証で拒否される
	for _, id := range []string{"../..", "a/../..", "/", "."} {
		if _, err := SanitizeJobID(id); err == nil {
			t.Errorf("SanitizeJobID(%q) = nil error, want rejection", id)
		}
	}
}

func TestJobOutputDir(t *testing.T) {
	t.Parallel()

	got, err := JobOutputDir("my-bucket", "c123-abc")
	if err != nil || got != "gs://my-bucket/comics/c123-abc" {
		t.Errorf("JobOutputDir = %q, %v; want gs://my-bucket/comics/c123-abc", got, err)
	}

	if _, err := JobOutputDir("my-bucket", "../.."); err == nil {
		t.Error("JobOutputDir(../..) succeeded, want error")
	}
}

func TestJobObjectPrefix(t *testing.T) {
	t.Parallel()

	got, err := JobObjectPrefix("c123-abc")
	if err != nil || got != "comics/c123-abc" {
		t.Errorf("JobObjectPrefix = %q, %v; want comics/c123-abc", got, err)
	}

	// パス風の値は Base で無害化される（結果にトラバーサルが残らない）
	got, err = JobObjectPrefix("../escape")
	if err != nil || got != "comics/escape" {
		t.Errorf("JobObjectPrefix(../escape) = %q, %v; want comics/escape (sanitized)", got, err)
	}

	// 無害化しても不正な値は拒否される
	if _, err := JobObjectPrefix("../.."); err == nil {
		t.Error("JobObjectPrefix(../..) succeeded, want error")
	}
}
