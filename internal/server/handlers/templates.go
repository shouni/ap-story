package handlers

import (
	"fmt"
	"html/template"
	"io/fs"

	"github.com/shouni/ap-story/assets"
)

// templateDir is the directory within the assets package that holds the templates.
const templateDir = "templates"

// loadTemplateCache loads the full set of embedded HTML templates.
// It associates layout.html with each page's template file and caches them.
func loadTemplateCache() (map[string]*template.Template, error) {
	entries, err := fs.ReadDir(assets.Templates, templateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read the template directory: %w", err)
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
			return nil, fmt.Errorf("failed to parse template %s: %w", pageName, err)
		}
		cache[pageName] = tmpl
	}
	return cache, nil
}
