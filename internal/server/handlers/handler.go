// Package handlers は、/api/* の JSON API と、Home 等のブラウザ向け画面(HTML)の
// HTTP ハンドラーを提供します。
package handlers

import (
	"fmt"
	"html/template"
	"strings"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// Handler は、API/Web UI ハンドラーが共有する依存関係を保持します。
type Handler struct {
	taskQueue  domain.TaskQueue
	repository domain.ComicRepository
	// jobStatus はジョブ進行状況の記録・参照先です。未設定なら状態機能は無効です。
	jobStatus  domain.JobStatusStore
	signer     remoteio.URLSigner
	bucket     string
	templates  map[string]*template.Template
	characters *characterkit.Characters
	// imageModels は、デザインシート生成フォームのモデル選択肢です（先頭が既定）。
	// 空なら選択肢を出しません。
	imageModels []string
}

// HandlerOptions は、NewHandler に渡す Handler の構築用オプションです。
type HandlerOptions struct {
	TaskQueue   domain.TaskQueue
	Repository  domain.ComicRepository
	JobStatus   domain.JobStatusStore
	Signer      remoteio.URLSigner
	Bucket      string
	Characters  *characterkit.Characters
	ImageModels []string
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
		taskQueue:   opts.TaskQueue,
		repository:  opts.Repository,
		jobStatus:   opts.JobStatus,
		signer:      opts.Signer,
		bucket:      bucket,
		templates:   templates,
		characters:  opts.Characters,
		imageModels: opts.ImageModels,
	}, nil
}
