// Package assets は、コードとは別のサイクルで編集される埋め込みリソース
// （HTML テンプレート・CSS/JavaScript・プロンプトテンプレート）を提供します。
// 姉妹プロジェクト（ap-comp / ap-mv）と同じ配置です。
//
// このパッケージは置き場に徹します。読み込み・パース・組み立ては利用側
// （internal/server/handlers、internal/adapters/prompts）の責務です。ここに処理を
// 足し始めると、埋め込みリソースの所在という単一の役割が失われます。
package assets

import "embed"

const (
	// OutlinePromptDir は章立て生成プロンプトの埋め込みパスです。
	OutlinePromptDir = "prompts/outline"
	// ChapterPromptDir は章台本生成プロンプトの埋め込みパスです。
	ChapterPromptDir = "prompts/chapter"
	// StylesJSONPath は画風プリセット（スタイルモード）の埋め込みパスです。
	// 台本プロンプトと違い、1件が画風指定とネガティブプロンプトの対で決まる
	// 構造データなので、テンプレートではなく JSON で持ちます。
	StylesJSONPath = "prompts/styles.json"
)

var (
	// Templates は、画面を構成する HTML テンプレート一式を保持します。
	//go:embed templates/*.html
	Templates embed.FS

	// StaticFiles は、ブラウザへ配信するCSS/JavaScriptなどの静的ファイルを保持します。
	//go:embed static
	StaticFiles embed.FS

	// Prompts は、台本生成プロンプトのテンプレートと画風プリセットを保持します。
	// テンプレートはディレクトリ名が生成工程、ファイル名（拡張子を除く）がモード名です。
	//go:embed prompts/outline/*.md prompts/chapter/*.md prompts/styles.json
	Prompts embed.FS
)
