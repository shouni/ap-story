// Package handlers は、JSON と HTML を同じルートで返す Web 面の
// HTTP ハンドラーを提供します。
package handlers

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"time"

	characterkit "github.com/shouni/go-character-kit/character"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/domain"
)

// Signer は、画像リダイレクトが必要とする署名機能だけを表します。
// remoteio.Store がそのまま満たします。
type Signer interface {
	SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error)
}

// Handler は、API/Web UI ハンドラーが共有する依存関係を保持します。
type Handler struct {
	taskQueue  domain.TaskQueue
	repository domain.ComicRepository
	// jobStatus はジョブ進行状況の記録・参照先です。未設定なら状態機能は無効です。
	jobStatus  domain.JobStatusStore
	signer     Signer
	bucket     string
	templates  map[string]*template.Template
	characters *characterkit.Characters
	// imageModels / geminiModels は、生成フォームのモデル選択肢です（先頭が既定）。
	// 空なら選択肢を出しません。JSON API の検証にも使う許可リストでもあります。
	imageModels  []string
	geminiModels []string
	// scriptModes / styleModes は、漫画生成フォームの台本モード・スタイルモードの
	// 選択肢です（assets/prompts 配下のテンプレートとその front matter）。
	scriptModes []prompts.ModeInfo
	styleModes  []prompts.ModeInfo
}

// HandlerOptions は、NewHandler に渡す Handler の構築用オプションです。
type HandlerOptions struct {
	TaskQueue    domain.TaskQueue
	Repository   domain.ComicRepository
	JobStatus    domain.JobStatusStore
	Signer       Signer
	Bucket       string
	Characters   *characterkit.Characters
	ImageModels  []string
	GeminiModels []string
	ScriptModes  []prompts.ModeInfo
	StyleModes   []prompts.ModeInfo
}

// NewHandler は指定された構成に基づいて新しいハンドラーを初期化します。
func NewHandler(opts HandlerOptions) (*Handler, error) {
	if opts.TaskQueue == nil {
		return nil, fmt.Errorf("task queue is required")
	}
	if opts.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if opts.Signer == nil {
		return nil, fmt.Errorf("signer is required")
	}
	bucket := strings.TrimSpace(opts.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	if opts.Characters == nil {
		return nil, fmt.Errorf("characters is required")
	}
	templates, err := loadTemplateCache()
	if err != nil {
		return nil, fmt.Errorf("failed to load HTML templates: %w", err)
	}
	return &Handler{
		taskQueue:    opts.TaskQueue,
		repository:   opts.Repository,
		jobStatus:    opts.JobStatus,
		signer:       opts.Signer,
		bucket:       bucket,
		templates:    templates,
		characters:   opts.Characters,
		imageModels:  opts.ImageModels,
		geminiModels: opts.GeminiModels,
		scriptModes:  opts.ScriptModes,
		styleModes:   opts.StyleModes,
	}, nil
}
