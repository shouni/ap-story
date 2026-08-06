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
	// imageStandardModel / imageQualityModel は、デザインシート生成フォームの
	// モデル選択肢として提示する既定モデル名です（空文字の場合は選択肢から除外）。
	imageStandardModel string
	imageQualityModel  string
}

// HandlerOptions は、NewHandler に渡す Handler の構築用オプションです。
type HandlerOptions struct {
	TaskQueue          domain.TaskQueue
	Repository         domain.ComicRepository
	JobStatus          domain.JobStatusStore
	Signer             remoteio.URLSigner
	Bucket             string
	Characters         *characterkit.Characters
	ImageStandardModel string
	ImageQualityModel  string
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
		taskQueue:          opts.TaskQueue,
		repository:         opts.Repository,
		jobStatus:          opts.JobStatus,
		signer:             opts.Signer,
		bucket:             bucket,
		templates:          templates,
		characters:         opts.Characters,
		imageStandardModel: strings.TrimSpace(opts.ImageStandardModel),
		imageQualityModel:  strings.TrimSpace(opts.ImageQualityModel),
	}, nil
}
