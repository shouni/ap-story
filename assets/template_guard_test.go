package assets

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// This file checks that the embedded templates keep meeting the CSP's assumptions.
//
// The CSP that secureheaders assembles sets script-src to 'self' only.
// A single inline <script> or on* attribute stops that screen's scripts from running,
// and the person who added the template cannot notice (the other screens work normally).
//
// This has actually happened in sibling apps. A survey of 5 apps on 2026-08-24 found
// inline scripts left only in ap-voice and adk-review, both apps without this kind of
// guard. The music generation service and ap-mv, which have it, had none.

// templateNames returns the paths of every embedded template.
func templateNames(t *testing.T) []string {
	t.Helper()

	var names []string
	err := fs.WalkDir(Templates, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".html" {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot list the templates: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no template found at all (suspect the embed directive)")
	}
	return names
}

// TestTemplatesHaveNoInlineScripts guarantees that behaviour stays outside the templates.
// Left inline, a screen's structure and behaviour mix in the same file, which invites
// embedding template values into JS string literals (onclick="del('{{.JobID}}')").
// It is also a precondition for keeping the CSP's script-src at 'self'.
func TestTemplatesHaveNoInlineScripts(t *testing.T) {
	t.Parallel()

	scriptTag := regexp.MustCompile(`<script\b[^>]*>`)
	// Pick up only on*= in attribute position (whitespace is required so English words in the text are not caught).
	eventAttribute := regexp.MustCompile(`\son[a-z]+\s*=\s*["']`)

	for _, name := range templateNames(t) {
		t.Run(name, func(t *testing.T) {
			body, err := Templates.ReadFile(name)
			if err != nil {
				t.Fatalf("cannot read: %v", err)
			}
			for _, tag := range scriptTag.FindAllString(string(body), -1) {
				if !strings.Contains(tag, "src=") {
					t.Errorf("an inline script remains: %s", tag)
				}
			}
			for _, attribute := range eventAttribute.FindAllString(string(body), -1) {
				t.Errorf("an event handler attribute remains: %s", strings.TrimSpace(attribute))
			}
		})
	}
}

// TestTemplatesReferenceNoExternalOrigins keeps Bootstrap served by the app itself.
// If a reference to an external origin came back, the CSP would drop it at run time, so CI catches it.
func TestTemplatesReferenceNoExternalOrigins(t *testing.T) {
	t.Parallel()

	external := regexp.MustCompile(`(?:href|src)="(https?://[^"]+)"`)

	for _, name := range templateNames(t) {
		t.Run(name, func(t *testing.T) {
			body, err := Templates.ReadFile(name)
			if err != nil {
				t.Fatalf("cannot read: %v", err)
			}
			for _, ref := range external.FindAllStringSubmatch(string(body), -1) {
				t.Errorf("a reference to an external origin has come back: %s", ref[1])
			}
		})
	}
}

// TestTemplateLocalAssetsExist checks that the /static/... paths the templates point at exist.
// A vendor version bump needs both the template and the directory name fixed, and fixing
// only one gives a 404 (unnoticed until opened in a browser).
func TestTemplateLocalAssetsExist(t *testing.T) {
	t.Parallel()

	ref := regexp.MustCompile(`(?:href|src)="/static/([^"?]+)`)

	for _, name := range templateNames(t) {
		body, err := Templates.ReadFile(name)
		if err != nil {
			t.Fatalf("cannot read: %v", err)
		}
		for _, match := range ref.FindAllStringSubmatch(string(body), -1) {
			asset := path.Join("static", match[1])
			if _, err := fs.Stat(StaticFiles, asset); err != nil {
				t.Errorf("%s points at %s, which does not exist: %v", name, asset, err)
			}
		}
	}
}
