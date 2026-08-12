package handlers

import (
	"log/slog"
	"net/http"
	"slices"

	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"
)

// ServeHistory は GET /history を処理し、ページング付きの履歴一覧画面を表示します。
// データ取得は JSON API（ListComics）と同一で、レスポンス形式だけが HTML になります。
func (h *Handler) ServeHistory(w http.ResponseWriter, r *http.Request) {
	page := parseHistoryPage(r)
	historyPage, err := h.repository.ListHistoryPage(r.Context(), page, defaultHistoryPageSize)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list comic history", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, http.StatusOK, "history.html", "作品履歴", historyPage)
}

// detailPanel は詳細画面の1パネル分の表示データです。
type detailPanel struct {
	ID        string
	Shot      string
	ImageURL  string
	Dialogues []string
}

// detailChapter は詳細画面の1章分の表示データです。
type detailChapter struct {
	ID      string
	Title   string
	Summary string
	Panels  []detailPanel
	// PendingPanels / PendingPages は、その章に未生成のコマ・ページが残っているかです。
	// ボタンを「コマを生成」と「ページを合成」で出し分けるために使います。
	// コマが揃うまでページ合成を出さないのは、ページがコマを並べた合成物だからです
	// （崩れたコマから2Kのページを作っても払い直しになります）。
	PendingPanels bool
	PendingPages  bool
}

// detailPage は詳細画面の1ページ分の表示データです。
type detailPage struct {
	Number   int
	ImageURL string
}

// detailDesignSheet は詳細画面のデザインシート1枚分の表示データです。
type detailDesignSheet struct {
	CharacterID string
	ImageURL    string
}

// historyDetailData は history_detail.html テンプレートに渡すデータです。
type historyDetailData struct {
	JobID        string
	Title        string
	Description  string
	UpdatedAt    string
	Chapters     []detailChapter
	Pages        []detailPage
	DesignSheets []detailDesignSheet
	// HasPendingImages は未生成のコマ・ページが残っているかです。
	// 「画像生成へ進む」（render_comic）ボタンの表示条件に使います。
	HasPendingImages bool
	// HasAnyImage は画像が1枚でも生成済みかです。ボタンの文言を
	// 「画像生成へ進む」と「続きを生成」で切り替えるために使います。
	HasAnyImage bool
	// PendingPanels / PendingPages は作品全体での未生成の有無です（章ごとの同名項目と同じ役割）。
	PendingPanels bool
	PendingPages  bool
	// ImageModels は画像モデルの選択肢です。台本の時点ではコマ数も絵柄も分からないので、
	// 選ぶのはここ（画像生成を始める画面）です。初期選択は state に記録された値で、
	// 1作品が途中から別のモデルにならないようにしています。
	ImageModels []selectOption
}

// historyPendingData は history_pending.html テンプレートに渡すデータです。
type historyPendingData struct {
	JobID string
}

// ServeDetails は GET /history/{jobID} を処理し、作品詳細画面
// （章立て・パネル・ページ・デザインシート）を表示します。
// state の取得は JSON API（GetComic）と同一で、レスポンス形式だけが HTML になります。
// state がまだ存在しない場合（生成中の直リンクなど）は、生の 404 ではなく
// 「処理中かもしれない」旨の案内画面を返します。
func (h *Handler) ServeDetails(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}

	state, err := h.repository.GetState(r.Context(), jobID)
	if err != nil {
		h.render(w, r, http.StatusNotFound, "history_pending.html", "処理中または未存在", historyPendingData{JobID: jobID})
		return
	}

	h.render(w, r, http.StatusOK, "history_detail.html", state.Title, h.buildDetailData(jobID, state))
}

// buildDetailData は MangaState を詳細画面用の表示データへ変換します。
// GCS 上の画像 URL（gs://）は、署名 URL へ 302 リダイレクトする既存の画像エンドポイント
// （/api/comics/{jobID}/images/*）のパスに変換します。
func (h *Handler) buildDetailData(jobID string, state *kitcomic.MangaState) historyDetailData {
	data := historyDetailData{
		JobID:       jobID,
		Title:       state.Title,
		Description: state.Description,
	}
	if !state.UpdatedAt.IsZero() {
		data.UpdatedAt = state.UpdatedAt.Format("2006-01-02 15:04 MST")
	}

	panelsByChapter := make(map[string][]detailPanel, len(state.Chapters))
	for _, p := range state.Panels {
		dp := detailPanel{ID: p.ID, Shot: p.Shot}
		if p.Generation != nil {
			dp.ImageURL = h.comicImageWebPath(jobID, p.Generation.ImageURL)
		}
		for _, d := range p.Dialogues {
			if d.SpeakerID == "" {
				dp.Dialogues = append(dp.Dialogues, d.Text)
			} else {
				dp.Dialogues = append(dp.Dialogues, d.SpeakerID+": "+d.Text)
			}
		}
		panelsByChapter[p.ChapterID] = append(panelsByChapter[p.ChapterID], dp)
	}
	for _, ch := range state.Chapters {
		data.Chapters = append(data.Chapters, detailChapter{
			ID:      ch.ID,
			Title:   ch.Title,
			Summary: ch.Summary,
			Panels:  panelsByChapter[ch.ID],
		})
	}

	for _, pg := range state.Pages {
		dp := detailPage{Number: pg.PageNumber}
		if pg.Generation != nil {
			dp.ImageURL = h.comicImageWebPath(jobID, pg.Generation.ImageURL)
		}
		data.Pages = append(data.Pages, dp)
	}
	// state.Pages は合成した順に並びます（go-comic-kit の SetPageArtifact は
	// 新規ページを末尾に足すだけ）。ページ3だけ先に作り直せば state 上は 3, 1, 2 に
	// なるので、読む順に直します。サムネイル一覧なら番号ラベルで気づけますが、
	// 通し読みでは順序が狂っていること自体に気づけません。
	slices.SortFunc(data.Pages, func(a, b detailPage) int { return a.Number - b.Number })

	for _, ds := range state.DesignSheets {
		data.DesignSheets = append(data.DesignSheets, detailDesignSheet{
			CharacterID: ds.CharacterID,
			ImageURL:    h.characterImageWebPath(ds.ImageURL),
		})
	}

	data.HasPendingImages, data.HasAnyImage = imageProgress(state)
	data.ImageModels = modelOptions(h.imageModels, state.ImageModel)
	data.PendingPanels, data.PendingPages = pendingImages(state, "")
	for i := range data.Chapters {
		data.Chapters[i].PendingPanels, data.Chapters[i].PendingPages = pendingImages(state, data.Chapters[i].ID)
	}
	return data
}

// pendingImages は、未生成のコマ・ページが残っているかを返します。
// chapterID が空なら作品全体、指定があればその章だけを見ます。
//
// ページを章で絞れるのは、Repaginate が章境界でページを割る（1ページに2章を混ぜない）
// ためです。go-comic-kit の PagesForChapter が同じ前提に乗っています。
func pendingImages(state *kitcomic.MangaState, chapterID string) (panels, pages bool) {
	targetPages := map[int]struct{}{}
	for i := range state.Panels {
		if chapterID != "" && state.Panels[i].ChapterID != chapterID {
			continue
		}
		targetPages[state.Panels[i].Page] = struct{}{}
		if state.Panels[i].Generation == nil {
			panels = true
		}
	}
	for page := range targetPages {
		artifact := state.PageArtifactByNumber(page)
		if artifact == nil || artifact.Generation == nil {
			pages = true
		}
	}
	return panels, pages
}

// imageProgress は、未生成の画像が残っているか・1枚でも生成済みかを返します。
// 台本だけの state（stop_after_script）と、途中で失敗した state のどちらでも
// 「続きを生成」へ誘導できるようにするための判定です。
func imageProgress(state *kitcomic.MangaState) (pending, generated bool) {
	pageCount := map[int]struct{}{}
	for i := range state.Panels {
		pageCount[state.Panels[i].Page] = struct{}{}
		if state.Panels[i].Generation == nil {
			pending = true
		} else {
			generated = true
		}
	}
	for page := range pageCount {
		artifact := state.PageArtifactByNumber(page)
		if artifact == nil || artifact.Generation == nil {
			pending = true
		} else {
			generated = true
		}
	}
	return pending, generated
}
