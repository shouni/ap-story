package handlers

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/go-serve-kit/respond"
)

// referenceAspectOrder is the display order of aspect ratios in character_detail.html.
// 3:4 comes first because panels and pages are generated at that ratio (go-comic-kit's
// layout.PanelAspectRatio / PageAspectRatio). A reference image with the same ratio as the target is
// preferred, so the 3:4 sheet is the one that matters for comic generation.
var referenceAspectOrder = []string{"3:4", "16:9", "9:16", "1:1"}

// characterListItem is one character's data passed to the characters.html template.
type characterListItem struct {
	ID                string
	Name              string
	ThumbnailImageURL string
}

// characterReferenceImage is one master reference image passed to character_detail.html.
type characterReferenceImage struct {
	Label    string
	ImageURL string
}

// characterHistoryImage is one generation record passed to character_detail.html.
type characterHistoryImage struct {
	JobID    string
	ImageURL string
}

// characterDetailHistoryLimit is the maximum number of generation records shown on the detail page.
// Anything beyond it is seen at /characters/{characterID}/history.
const characterDetailHistoryLimit = 12

// characterDetailData is the data passed to the character_detail.html template.
type characterDetailData struct {
	CharacterID string
	Name        string
	// References are the master reference images (the default + per aspect ratio). They are the "reference"
	// look used as generation input, managed separately from the AI-generated design-sheet history.
	References []characterReferenceImage
	// History is the AI-generated design-sheet history (newest first, at most characterDetailHistoryLimit items).
	// Sheets generated for several characters together are not shown here
	// (they are recorded only under character/{single character ID}/).
	History []characterHistoryImage
	// HistoryTotal is the total history count before the limit. When it is larger than History,
	// the template links to the page showing everything.
	HistoryTotal int
}

// characterHistoryPageData is the data passed to the character_history.html template.
type characterHistoryPageData struct {
	CharacterID string
	Name        string
	History     []characterHistoryImage
}

// CharacterDesignSheets handles GET /characters/{characterID}/design-sheets and shows
// the whole AI-generated design-sheet history (newest first).
func (h *Handler) CharacterDesignSheets(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	char := h.characters.GetCharacter(characterID)
	if char == nil {
		http.Error(w, "character not found", http.StatusNotFound)
		return
	}

	history, err := h.buildCharacterHistoryImages(r, characterID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, http.StatusOK, "character_history.html", char.Name+" の生成履歴", characterHistoryPageData{
		CharacterID: characterID,
		Name:        char.Name,
		History:     history,
	})
}

// DeleteCharacterDesign handles DELETE /characters/{characterID}/design-sheets/{jobID} and deletes
// one generation record of the character (the image, and the state too for a standalone job).
func (h *Handler) DeleteCharacterDesign(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	if h.characters.GetCharacter(characterID) == nil {
		respond.Error(w, r, http.StatusNotFound, "character not found")
		return
	}
	jobID := chi.URLParam(r, "jobID")

	if err := h.repository.DeleteCharacterDesign(r.Context(), characterID, jobID); err != nil {
		respond.ServerError(w, r, http.StatusInternalServerError, err, "failed to delete character design",
			"character_id", characterID, "job_id", jobID)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// buildCharacterReferences converts the master reference images (the default + per aspect ratio, sorted
// for display) into template data.
func (h *Handler) buildCharacterReferences(referenceURL string, referenceURLs map[string]string) []characterReferenceImage {
	var refs []characterReferenceImage
	if url := h.characterReferenceWebPath(referenceURL); url != "" {
		refs = append(refs, characterReferenceImage{Label: "既定", ImageURL: url})
	}
	aspectRatios := make([]string, 0, len(referenceURLs))
	for ratio := range referenceURLs {
		aspectRatios = append(aspectRatios, ratio)
	}
	sort.Slice(aspectRatios, func(i, j int) bool {
		return referenceAspectIndex(aspectRatios[i]) < referenceAspectIndex(aspectRatios[j])
	})
	for _, ratio := range aspectRatios {
		if url := h.characterReferenceWebPath(referenceURLs[ratio]); url != "" {
			refs = append(refs, characterReferenceImage{Label: ratio, ImageURL: url})
		}
	}
	return refs
}

// buildCharacterHistoryImages converts the generation history (newest first, everything) into template data.
// On a failure it logs and returns an error (the HTTP response is the caller's job).
func (h *Handler) buildCharacterHistoryImages(r *http.Request, characterID string) ([]characterHistoryImage, error) {
	items, err := h.repository.ListCharacterDesignHistory(r.Context(), characterID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list character design history",
			"character_id", characterID, "error", err)
		return nil, err
	}
	history := make([]characterHistoryImage, 0, len(items))
	for _, item := range items {
		history = append(history, characterHistoryImage{
			JobID:    item.JobID,
			ImageURL: h.characterImageWebPath(item.ImageURL),
		})
	}
	return history, nil
}

// referenceAspectIndex returns the position within referenceAspectOrder (unknown ratios go last).
func referenceAspectIndex(ratio string) int {
	for i, r := range referenceAspectOrder {
		if r == ratio {
			return i
		}
	}
	return len(referenceAspectOrder)
}
