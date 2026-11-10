package adapters

import (
	"context"
	"fmt"
	"io"

	"github.com/shouni/go-character-kit/assets"
	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-remote-io/remoteio"
)

// LoadCharacters loads the character definitions from CHARACTERS_JSON_PATH (a GCS URI or a
// local path). They are loaded at run time rather than embedded (go:embed) so that adding or
// updating a character needs no redeploy.
// When path is empty, go-character-kit's embedded default character definitions are used.
func LoadCharacters(ctx context.Context, reader remoteio.Reader, path string) (*characterkit.Characters, error) {
	if path == "" {
		characters, err := assets.LoadCharacters()
		if err != nil {
			return nil, fmt.Errorf("failed to load the default character definitions: %w", err)
		}
		return characters, nil
	}

	rc, err := reader.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to open characters.json (%s): %w", path, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read characters.json (%s): %w", path, err)
	}

	characters, err := characterkit.ParseCharacters(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse characters.json (%s): %w", path, err)
	}

	return characters, nil
}
