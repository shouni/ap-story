package handlers

import (
	"slices"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

// detailPanel is the display data of one panel on the detail page.
type detailPanel struct {
	ID        string
	Shot      string
	ImageURL  string
	Dialogues []string
}

// detailChapter is the display data of one chapter on the detail page.
type detailChapter struct {
	ID      string
	Title   string
	Summary string
	Panels  []detailPanel
	// PendingPanels / PendingPages say whether the chapter still has panels or pages not yet generated.
	// They are used to choose between the "コマを生成" and "ページを合成" buttons.
	// Page composition is not offered until the panels are all there because a page is a composite of the panels
	// (making a 2K page from broken panels means paying again).
	PendingPanels bool
	PendingPages  bool
}

// detailPage is the display data of one page on the detail page.
type detailPage struct {
	Number   int
	ImageURL string
}

// detailDesignSheet is the display data of one design sheet on the detail page.
type detailDesignSheet struct {
	CharacterID string
	ImageURL    string
}

// historyDetailData is the data passed to the history_detail.html template.
type historyDetailData struct {
	JobID        string
	Title        string
	Description  string
	UpdatedAt    string
	Chapters     []detailChapter
	Pages        []detailPage
	DesignSheets []detailDesignSheet
	// HasPendingImages says whether panels or pages not yet generated remain.
	// It is the condition for showing the "画像生成へ進む" (render_comic) button.
	HasPendingImages bool
	// HasAnyImage says whether even one image has been generated. It switches the button label
	// between "画像生成へ進む" and "続きを生成".
	HasAnyImage bool
	// PendingPanels / PendingPages say whether anything remains ungenerated across the work (the same role as the per-chapter fields).
	PendingPanels bool
	PendingPages  bool
	// ImageModels are the image model options. At the script stage neither the panel count nor the look is known,
	// so they are chosen here (the page that starts image generation). The initial choice is the value recorded
	// in the state, so one work does not switch to a different model midway.
	ImageModels []selectOption
}

// historyPendingData is the data passed to the history_pending.html template.
type historyPendingData struct {
	JobID string
}

// buildDetailData converts a MangaState into the detail page's display data.
// GCS image URLs (gs://) are converted into the paths of the existing image endpoint that 302-redirects
// to a signed URL (/api/comics/{jobID}/images/*).
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
	// state.Pages is in composition order (go-comic-kit's SetPageArtifact just appends
	// a new page at the end). Remaking only page 3 first makes the state order 3, 1, 2,
	// so they are put in reading order. A thumbnail list would show it through the number labels, but
	// reading straight through one would not even notice the order is wrong.
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

// pendingImages reports whether panels or pages not yet generated remain.
// With chapterID empty it looks at the whole work; otherwise only at that chapter.
//
// Pages can be narrowed by chapter because Repaginate splits pages at chapter boundaries (never mixing two
// chapters on one page). go-comic-kit's PagesForChapter relies on the same premise.
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

// imageProgress reports whether ungenerated images remain and whether even one has been generated.
// It is the check that leads to "続きを生成" both for a script-only state (stop_after_script)
// and for a state that failed midway.
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
