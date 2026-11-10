package repository

import (
	"context"
	"fmt"
	"sort"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-utils/jobid"

	"github.com/shouni/ap-story/internal/domain"
)

// ListCharacterDesignHistory returns the design-sheet generation history of one character,
// newest first. It lists character/{characterID}/ on GCS directly and so does not read
// comic_state.json. Job IDs (e.g. "c20260718-113045-1a2b3c4d") include the creation time,
// so a descending string sort is newest first as is.
func (r *ComicRepository) ListCharacterDesignHistory(ctx context.Context, characterID string) ([]domain.CharacterDesignHistoryItem, error) {
	listURI, err := asset.CharacterDesignPrefix(remoteio.BuildURI(remoteio.SchemeGCS, r.bucket, ""), characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to build the listing path for character images: %w", err)
	}

	var items []domain.CharacterDesignHistoryItem
	for entry, err := range r.store.List(ctx, listURI) {
		if err != nil {
			return nil, fmt.Errorf("failed to list the character image history: %w", err)
		}
		// The file-naming convention belongs to go-comic-kit. Reverse-engineering it here would make
		// the list go empty, with no error, when the kit changes how it names files.
		jobID, ok := asset.DesignSheetJobID(listURI, entry.URI)
		if !ok {
			continue
		}
		items = append(items, domain.CharacterDesignHistoryItem{
			JobID:    jobID,
			ImageURL: entry.URI,
		})
	}

	// Sort by the creation time embedded in the ID rather than lexically (old and new ID formats are mixed).
	sort.Slice(items, func(i, j int) bool {
		ki, kj := jobid.SortKey(items[i].JobID), jobid.SortKey(items[j].JobID)
		if ki != kj {
			return ki > kj
		}
		return items[i].JobID > items[j].JobID
	})
	return items, nil
}
