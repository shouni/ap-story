// Package domain は、ap-story のフレームワーク非依存なドメインロジックを提供します。
package domain

import (
	"fmt"
	"path"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-utils/jobid"
)

// jobIDPrefix は ap-story が採番するジョブ ID のプレフィックスです。
const jobIDPrefix = "c"

// comicsPrefix は作品ジョブの成果物を格納する GCS 上のルートプレフィックスです。
const comicsPrefix = "comics"

// designJobsPrefix はデザインシート単体生成ジョブの state を格納する GCS 上の
// ルートプレフィックスです。comics/ と分けることで、履歴一覧（comics/ の列挙）が
// state を読まずに「作品ジョブだけ」を列挙でき、ページング前の全件読み込みを防ぎます。
const designJobsPrefix = "design-jobs"

// NewJobID は新しいジョブ ID を採番します。
// 形式は "c-<UTC日付>-<UTC時刻>-<乱数12桁hex>"（例: "c-20260717-113045-1a2b3c4d5e6f"）です。
//
// 採番規則は検証規則と同じく go-utils/jobid に集約しています。生成と、ID に埋め込まれた
// 時刻の復元（jobid.CreatedAt / jobid.SortKey）が同じパッケージにあることで、片方だけ
// 形式が変わって読めなくなる事故を防げます。
//
// v1.5.0 以前は "c<UTC日付>-<UTC時刻>-<乱数8桁hex>" 形式で採番していました。
// 過去に採番された ID は GCS の成果物パスに残り続けますが、jobid.CreatedAt が
// 両方の形式を読めるため、履歴の日時表示と並び順は混在しても保たれます。
// ただし辞書順の比較では新旧が分離してしまうため、並べ替えには必ず
// jobid.SortKey を使ってください（"c-" と "c2" では前者が小さくなります）。
func NewJobID() (string, error) {
	return jobid.New(jobIDPrefix)
}

// ValidateJobID は、ジョブ ID がルーティングとストレージパスで安全に使えることを検証します。
// HTTP 入力と GCS パス生成は必ずこの検証（または SanitizeJobID）を経由してください。
//
// 検証規則は go-utils/jobid に集約しています。MCP サーバーを介して姉妹プロジェクトとも
// ジョブ ID をやり取りするため、「何を正当な ID とみなすか」がサービス間でずれると
// 片方が発行した ID をもう片方が拒否する事故につながるためです。
func ValidateJobID(jobID string) error {
	return jobid.Validate(jobID)
}

// SanitizeJobID は、パス風の値を安全なジョブ ID に正規化します。
// パストラバーサル（"../" 等）は末尾要素の抽出で除去され、残った値を検証します。
func SanitizeJobID(jobID string) (string, error) {
	return jobid.Sanitize(jobID)
}

// JobObjectPrefix は、検証済みジョブ ID の成果物を格納する GCS プレフィックス
// （バケット名を除く相対パス）を返します。例: "comics/c20260717-113045-1a2b3c4d"
func JobObjectPrefix(jobID string) (string, error) {
	safeJobID, err := SanitizeJobID(jobID)
	if err != nil {
		return "", err
	}
	return path.Join(comicsPrefix, safeJobID), nil
}

// JobOutputDir は、検証済みジョブ ID の成果物を格納する完全な GCS URI
// （例: "gs://bucket/comics/c20260717-113045-1a2b3c4d"）を返します。
// go-comic-kit の各操作（GenerateOptions.OutputDir 等）にそのまま渡せます。
func JobOutputDir(bucket, jobID string) (string, error) {
	prefix, err := JobObjectPrefix(jobID)
	if err != nil {
		return "", err
	}
	return remoteio.BuildURI(remoteio.SchemeGCS, bucket, prefix), nil
}

// DesignJobOutputDir は、デザインシート単体生成ジョブの state を格納する完全な GCS URI
// （例: "gs://bucket/design-jobs/c20260717-113045-1a2b3c4d"）を返します。
// 生成画像そのものは CharacterAssetDir 配下（character/{タグ}/）に保存されるため、
// ここに置かれるのは comic_state.json だけです。
func DesignJobOutputDir(bucket, jobID string) (string, error) {
	safeJobID, err := SanitizeJobID(jobID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("gs://%s/%s", bucket, path.Join(designJobsPrefix, safeJobID)), nil
}

// CharacterAssetDir は、特定のジョブ（作品）に依存しない共有キャラクター資産
// （デザインシート等）の保存先となる GCS バケットルート URI（例: "gs://bucket"）を返します。
// キャラクターは複数の作品から参照されうる共有アセットのため、comics/{jobID}/ の外に置きます。
// go-comic-kit の DesignSheetRequest.OutputDir に渡すと、
// "character/{タグ}/{JobID}.ext" として解決されます。
func CharacterAssetDir(bucket string) string {
	return remoteio.BuildURI(remoteio.SchemeGCS, bucket, "")
}
