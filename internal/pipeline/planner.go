package pipeline

import (
	"fmt"

	"github.com/shouni/ap-story/internal/domain"
)

// Planner は、Task に応じて実行すべきステップ列（実行計画）を決定します。
// Runner は計画の取得と順次実行だけを担い、コマンドごとのステップ構成の知識は
// Planner 側に集約されます。
type Planner interface {
	Plan(task *domain.Task) ([]Step, error)
}

// DefaultPlanner は、コマンドごとの標準ステップ列を返す本番用の Planner です。
type DefaultPlanner struct{}

// Plan はコマンドに応じた標準ステップ列を返します。各ケースがそのコマンドの
// 完全なステップ列を返すため、コマンドごとの違いは1箇所を見れば分かります。
//
// compose_comic は工程（台本→パネル→ページ）の切れ目ごとに SaveStateStep で保存し、
// 各工程は済んでいる分を飛ばします。compose_comic はジョブ ID を新規採番したときにしか
// 投入されないため、state がすでに存在する = Cloud Tasks の再配信による再実行であり、
// そこで最初からやり直すと保存済みの生成物を捨てて画像生成のコストを二重に払うことに
// なります。regenerate 系はいずれも対象 state をロードし、該当箇所だけを更新して同じ
// 場所に上書き保存します（go-comic-kit の各操作が *MangaState を直接書き換えて返すため、
// これで冪等な再生成が成立します）。
func (DefaultPlanner) Plan(task *domain.Task) ([]Step, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	switch task.Command {
	case domain.TaskCommandComposeComic:
		// 工程の切れ目ごとに保存する。画像生成は1ジョブで何十回も走るため、
		// 最後にまとめて保存すると途中の失敗で成果が丸ごと失われる。
		steps := []Step{
			LoadStateIfExistsStep{},
			OutlineStep{},
			AllChapterScriptsStep{},
			SaveStateStep{},
		}
		if task.StopAfterScript {
			// 台本を確認してから画像生成に進む運用。続きは render_comic が担当する。
			return steps, nil
		}
		return append(steps, renderSteps(true)...), nil
	case domain.TaskCommandRenderComic:
		// 既存 state の未生成分だけを埋める。台本確認後の「続きを生成」と、
		// 失敗・打ち切りからの再開の両方がこの1コマンドで済む。
		steps := []Step{LoadStateStep{}}
		if task.StopAfterPanels {
			// コマの出来を見てからページへ進む運用。ページ合成は
			// 同じコマンドをもう一度投げれば走る（生成済みのコマは飛ばされる）。
			return append(steps, AllPanelsStep{SkipGenerated: true}, SaveStateStep{}), nil
		}
		return append(steps, renderSteps(true)...), nil
	case domain.TaskCommandRegenerateChapterScript:
		return []Step{
			LoadStateStep{},
			SingleChapterScriptStep{},
			SaveStateStep{},
		}, nil
	case domain.TaskCommandGenerateDesignSheet:
		return []Step{
			LoadStateStepOptional{},
			DesignSheetStep{},
			SaveStateStep{},
		}, nil
	case domain.TaskCommandRegeneratePanel:
		return []Step{
			LoadStateStep{},
			SinglePanelStep{},
			SaveStateStep{},
		}, nil
	case domain.TaskCommandRegeneratePage:
		return []Step{
			LoadStateStep{},
			SinglePageStep{},
			SaveStateStep{},
		}, nil
	default:
		return nil, fmt.Errorf("no step plan for command %q", task.Command)
	}
}

// renderSteps は画像生成フェーズ（パネル→ページ）のステップ列を返します。
// skipGenerated が true の場合、すでに生成済みのコマ・ページは飛ばします。
func renderSteps(skipGenerated bool) []Step {
	return []Step{
		AllPanelsStep{SkipGenerated: skipGenerated},
		SaveStateStep{},
		AllPagesStep{SkipGenerated: skipGenerated},
		SaveStateStep{},
	}
}
