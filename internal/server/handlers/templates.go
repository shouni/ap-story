package handlers

import (
	"fmt"
	"html/template"
	"io/fs"

	"github.com/shouni/ap-story/assets"
)

// templateDir は assets パッケージ内でのテンプレートの配置ディレクトリです。
const templateDir = "templates"

// loadTemplateCache は埋め込み済みの HTML テンプレート一式を読み込みます。
// ページごとのテンプレートファイルに layout.html を関連付けてキャッシュします。
func loadTemplateCache() (map[string]*template.Template, error) {
	entries, err := fs.ReadDir(assets.Templates, templateDir)
	if err != nil {
		return nil, fmt.Errorf("テンプレートディレクトリの読み込みに失敗しました: %w", err)
	}

	layoutPath := templateDir + "/layout.html"
	cache := make(map[string]*template.Template, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "layout.html" {
			continue
		}

		pageName := entry.Name()
		pagePath := templateDir + "/" + pageName
		tmpl, err := template.New(pageName).ParseFS(assets.Templates, layoutPath, pagePath)
		if err != nil {
			return nil, fmt.Errorf("テンプレート %s の解析に失敗しました: %w", pageName, err)
		}
		cache[pageName] = tmpl
	}
	return cache, nil
}
