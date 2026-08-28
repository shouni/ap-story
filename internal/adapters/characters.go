package adapters

import (
	"context"
	"fmt"
	"io"

	"github.com/shouni/go-character-kit/assets"
	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-remote-io/remoteio"
)

// LoadCharacters は、CHARACTERS_JSON_PATH（GCS URI またはローカルパス）から
// キャラクター定義を読み込みます。埋め込み（go:embed）ではなく
// 実行時にロードするのは、キャラクター追加・更新のたびに再デプロイを不要にするためです。
// path が空文字の場合は go-character-kit の埋め込みデフォルトキャラクター定義を使います。
func LoadCharacters(ctx context.Context, reader remoteio.Reader, path string) (*characterkit.Characters, error) {
	if path == "" {
		characters, err := assets.LoadCharacters()
		if err != nil {
			return nil, fmt.Errorf("デフォルトのキャラクター定義の読み込みに失敗しました: %w", err)
		}
		return characters, nil
	}

	rc, err := reader.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("characters.json のオープンに失敗しました (%s): %w", path, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("characters.json の読み込みに失敗しました (%s): %w", path, err)
	}

	characters, err := characterkit.ParseCharacters(data)
	if err != nil {
		return nil, fmt.Errorf("characters.json のパースに失敗しました (%s): %w", path, err)
	}

	return characters, nil
}
