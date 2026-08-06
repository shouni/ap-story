# 🎨 AP Story

[![CI](https://github.com/shouni/ap-story/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/ap-story/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 🚀 概要 (About)

**AP Story** は、[go-comic-kit](https://github.com/shouni/go-comic-kit) を用いた
**MCP 対応の漫画生成オーケストレータサービス**（Cloud Run + Cloud Tasks）です。

原稿（URL / テキスト）から「章立て → ネーム → キャラクターデザインシート → パネル →
ページ」を非同期ジョブとして生成し、成果物と状態ドキュメント（`comic_state.json`）を
GCS に永続化します。ブラウザは **Google OAuth セッション**、AI エージェント（Claude 等）は
**サービスアカウントの OIDC**（M2M）のいずれかで同一の JSON API を呼び出せ、パネル単位の
再生成（シード振り直し・編集指示）まで対応します。MCP ツールは ap-mcp に統合済みで、
Claude などのエージェントから直接呼び出せます。

## ✨ 特徴 (Features)

* **🔁 非同期ジョブ + 再生成**: 生成はすべて Cloud Tasks 経由。`comic_state.json` が
  唯一の真実源で、「12パネル中3番だけシードを振り直して再生成」「表情だけ編集指示で修正」が
  ジョブとして投入できます。
* **📝 台本ゲート**: `stop_after_script` で章立て・ネームまでを先に生成し、内容を確認してから
  画像生成へ進めます。コマ数ぶんの画像生成は高価なため、確信が持てない原稿ではこの2段構えが有効です。
* **⏭️ 途中から再開**: state は工程の切れ目ごとに保存され、失敗時もそこまでの成果を残します。
  `render_comic` は未生成のコマ・ページだけを埋めるため、失敗したジョブを最初から作り直しません。
* **🔐 Google OAuth + M2M の二本立て**: 人間はブラウザから Google アカウントでログインして
  API を呼び出せ、AI エージェントはサービスアカウントの OIDC（M2M）で同じ API を呼び出せます
  （[gcp-kit](https://github.com/shouni/gcp-kit) の認証基盤を利用、ap-comp で実証済み）。
* **🧬 キャラクターの一貫性**: デザインシートを同一性アンカーとして、go-comic-kit の
  3-Factor Consistency Control（Seed / 参照アセット / VisualCues）で維持します。
* **📣 Slack 通知 + Cloud Build**: ジョブの完了・失敗を Slack Webhook で通知（任意設定）。
  `cloudbuild.yaml` で単一の Cloud Run サービスとしてビルド・デプロイします。

## 🏗️ アーキテクチャ

生成ロジック（台本・デザインシート・パネル・ページ）はすべて
[go-comic-kit](https://github.com/shouni/go-comic-kit) の操作セットに委譲し、ap-story は
**ジョブ管理・非同期実行・履歴・API/MCP 公開**に責務を絞ります。

### 非同期ジョブモデル

漫画生成は多段・長時間（数分〜十数分）のため、すべて Cloud Tasks 経由の非同期ジョブです。

1. `POST /api/comics` がジョブ ID を採番し、Cloud Tasks にタスクを enqueue して**即座に jobID を返す**
2. Worker（`POST /tasks/generate`）がタスクを受け、go-comic-kit の操作を実行して
   **state（`comic_state.json`）を GCS に保存**する
3. クライアントは `GET /api/comics/{jobID}/status`（ジョブ状態）と
   `GET /api/comics/{jobID}`（state の読み出し）で進捗・結果を確認する

冪等性は、Cloud Tasks の **task name にジョブ ID + 工程を含めて重複 enqueue を排除**し、
state の保存は「常に上書き・常に最新」（go-comic-kit `store.Save` の仕様）で担保しています。

`compose_comic` の各工程は済んでいる分を飛ばすため、再配信による再実行は未了分だけを進めます
（`compose_comic` はジョブ ID を新規採番したときにしか投入されないので、state が既にある
= 再実行、と判断できます）。最初からやり直すと、保存済みの生成物を捨てて画像生成のコストを
二重に払うことになります。

ただし `render_comic` だけは task name に投入時刻を含めて重複排除の対象外にしています。
このコマンドは「失敗したところから再開する」ためのもので、決定的な名前にすると正当な再開が
ALREADY_EXISTS として黙って捨てられます（Cloud Tasks の重複排除は完了後もしばらく効くため、
長時間ジョブが失敗した直後の再実行がまさに該当します）。生成済みは飛ばすので重ねて実行しても
無駄にはなりません。

### GCS レイアウト

```
gs://{STORY_BUCKET}/
├── comics/{jobID}/                    # 作品ジョブごとのプレフィックス
│   ├── comic_state.json               # MangaState（唯一の真実源。履歴・詳細はこれを読む）
│   └── images/
│       ├── panel_{panelID}.png        # パネル画像（パネルIDに紐づく安定パス・上書き）
│       └── comic_page_{N}.png         # ページ画像
├── design-jobs/{jobID}/
│   └── comic_state.json               # デザインシート単体生成ジョブの state（画像は character/ 側）
└── character/{tag}/{jobID}.png        # デザインシート（作品に依存しない共有アセット）
```

キャラクターのデザインシートは特定の作品（ジョブ）に属さない共有アセットのため、
`comics/{jobID}/` の外（バケット直下の `character/`）に置きます。`{tag}` はキャラクターID
（合成シートの場合は複数ID連結）、`{jobID}` は生成呼び出しごとに一意なので、同じキャラクターへの
再生成が過去の生成結果を上書きしません。

履歴一覧は `comics/*/comic_state.json` の列挙（ページング + TTL キャッシュ）です。デザインシート
単体生成ジョブの state は `design-jobs/` 側に保存されるため `comics/` の列挙に現れず、ID の列挙・
ページングは state を読まずに完結します（state を読むのは選択されたページの分だけ）。単体生成の
履歴は `/characters/{characterID}` 側で確認します。画像はアプリから直接配信せず、
**GCS 署名 URL へ 302 リダイレクト**します（パネル・ページは `/api/comics/{jobID}/images/*`、
デザインシート生成履歴は `/api/characters/images/*`）。jobID は必ず検証・サニタイズした
ものだけを HTTP 入力・GCS パス生成の両方で使います。

キャラクターのマスター参照画像（characters.json の `reference_url` / `reference_urls`、AI 生成では
なく人手で用意した正典画像）はバケット直下の `character-reference/{characterID}/...` に置き、
`character/`（AI 生成履歴）とはディレクトリを分けています。こちらは `/api/characters/reference/*`
経由で配信します。

## 📋 パイプラインコマンド（Task.command）

Worker が受けるタスクはコマンドで分岐します。各コマンドは「state をロード →
go-comic-kit の操作を実行 → state を保存」の形で、go-comic-kit の冪等な操作と1対1です。

state は工程（台本 → パネル → ページ）の切れ目ごとに保存され、ステップが失敗した場合も
そこまでの成果を保存してから終了します。したがって再実行は `render_comic` により未生成分
だけで済み、失敗したジョブの画像を作り直す必要はありません。

| command | 実行する go-comic-kit 操作 | 入力パラメータ |
|---|---|---|
| `compose_comic` | 全工程: GenerateOutline → 各章 GenerateChapterScript → GenerateAllPanels → ComposeAllPages（デザインシートは含まない。単体生成 `generate_design_sheet` で別途作成）。`stop_after_script` を指定すると台本までで止まる | source_url / source_text, script_mode, style_mode, stop_after_script |
| `render_comic` | GenerateAllPanels → ComposeAllPages（生成済みは飛ばす）。台本確認後の「続きを生成」と、失敗・打ち切りからの再開を兼ねる | job_id |
| `regenerate_chapter_script` | GenerateChapterScript（1章。後続のパネル・ページは別途再生成） | job_id, chapter_id |
| `generate_design_sheet` | GenerateDesignSheet | job_id（省略時は state なしの単発生成）, character_ids, aspect_ratio, layout, seed |
| `regenerate_panel` | GeneratePanel | job_id, panel_id, seed / edit_prompt / prompt_override |
| `regenerate_page` | ComposePage | job_id, page, seed / edit_prompt |

## ✍️ 生成フロー（Web UI）

1. `/compose` で原稿を投入する。**「台本まで生成して確認する」**にチェックを入れると、
   章立てとネームだけが作られる（画像生成のコストを払う前に内容を確認できる）。
2. `/history/{jobID}` で台本を確認する。問題なければ**「画像生成へ進む」**で `render_comic`
   を投入する。途中で失敗した場合も同じボタンが**「続きを生成」**になり、未生成分だけを埋める。
3. 気に入らないコマ・ページは詳細画面から個別に直す。
   * **シャッフル**（<code>bi-shuffle</code>）: シードを振り直して別の絵にする。
   * **鉛筆**（<code>bi-pencil</code>）: 「表情を笑顔に」のような指示で、構図を保ったまま部分編集する
     （生成済み画像が入力になるため、未生成のコマには表示されない）。
   * 章見出しの**「台本を再生成」**: その章のネームだけを作り直す（コマは作り直しになる）。

## 🌐 HTTP エンドポイント

`/api/*` はブラウザセッションまたは M2M（OIDC Bearer）のいずれかで呼び出せます。ブラウザ向けの
画面（HTML）は ap-comp と同様の構成（`html/template` + go:embed、静的アセットは `/static/*`）で
本リポジトリ内に実装しています。画面のハンドラは JSON API とコアロジックを共有し、
レスポンス形式（HTML/JSON）だけが異なります。

| メソッド/パス | 認証 | 内容 |
|---|---|---|
| `GET /auth/login`, `GET /auth/callback` | Google OAuth | ログインフロー（gcp-kit auth.Handler） |
| `GET /` | セッション or M2M | Home 画面（直近の作品を数件表示。未認証時は `/auth/login` へリダイレクト） |
| `GET /compose`, `POST /compose` | セッション | 漫画生成フォームの表示・投入（受付画面を返す） |
| `GET /design-sheets`, `POST /design-sheets` | セッション | デザインシート単体生成フォームの表示・投入（job_id は自動採番。`?character_id=`で事前選択可、画像モデル・参照画像URL・見た目特徴の上書きに対応） |
| `GET /characters` | セッション | キャラクター一覧（マスター参照画像のサムネイル付き） |
| `GET /characters/{characterID}` | セッション | キャラクター詳細（上段: サイズ/アスペクト比ごとのマスター参照画像、下段: デザインシート生成履歴の最新12件+削除ボタン。新しい順、合成生成は対象外） |
| `GET /characters/{characterID}/history` | セッション | デザインシート生成履歴の全件表示（新しい順、削除ボタン付き） |
| `GET /history` | セッション | 作品一覧画面（ナビ表記は Works。ページング・削除ボタン） |
| `GET /history/{jobID}` | セッション | 作品詳細画面（章・パネル・ページ・デザインシートの閲覧） |
| `GET /static/*` | なし | CSS/JS 静的アセット |
| `POST /api/comics` | セッション or M2M | compose_comic ジョブの投入（jobID を返す） |
| `GET /api/comics` | セッション or M2M | 履歴一覧（state の列挙、ページング） |
| `GET /api/comics/{jobID}` | セッション or M2M | 詳細（comic_state.json の内容） |
| `POST /api/comics/{jobID}/regenerate` | セッション or M2M | 再生成ジョブの投入（command + パラメータ） |
| `GET /api/comics/{jobID}/images/*` | セッション or M2M | パネル・ページ画像への署名 URL リダイレクト |
| `POST /api/design-sheets` | セッション or M2M | generate_design_sheet ジョブの投入（jobID は自動採番。character_ids は必須、aspect_ratio・layout・model_override・reference_url_override・visual_cues_override は任意） |
| `GET /api/characters` | セッション or M2M | キャラクター一覧（id・name・reference_url を返す。画像 URL は gs:// のまま） |
| `GET /api/characters/{characterID}` | セッション or M2M | キャラクター詳細（マスター参照画像 + 生成履歴全件、新しい順） |
| `GET /api/characters/images/*` | セッション or M2M | デザインシート生成履歴の画像への署名 URL リダイレクト（作品非依存） |
| `GET /api/characters/reference/*` | セッション or M2M | キャラクターのマスター参照画像への署名 URL リダイレクト |
| `DELETE /api/characters/{characterID}/images/{jobID}` | セッション or M2M | デザインシート生成履歴1件の削除（単体生成ジョブなら state も削除、作品ジョブは state 内の参照のみ除去） |
| `DELETE /api/comics/{jobID}` | セッション or M2M | ジョブ成果物の削除 |
| `POST /tasks/generate` | Cloud Tasks OIDC | Worker: コマンド実行 |
| `GET /health` | なし | ヘルスチェック |

## ⚙️ 環境変数

| 変数 | 内容 |
|---|---|
| `PORT` | HTTP ポート（Cloud Run 既定 8080） |
| `SERVER_ROLE` | プロセスが担う役割。`web` / `worker` / 未指定（両方）。詳細は「web / worker の分離」を参照 |
| `SERVICE_URL` | 自サービスの**公開** URL。OAuth のリダイレクト先、M2M 認証の audience、Slack 通知リンクの生成元を兼ねるため、worker にも**非公開の worker 自身ではなく web の URL** を設定する |
| `WORKER_URL` | Cloud Tasks が呼び出す Worker エンドポイント（省略時は SERVICE_URL 由来）。web 面のみ使用 |
| `STORY_BUCKET` | 成果物・state の GCS バケット |
| `CHARACTERS_JSON_PATH` | go-character-kit の characters.json（GCS/ローカル、任意。未設定時は go-character-kit 埋め込みの既定キャラクター定義を使用） |
| `GEMINI_MODEL` / `IMAGE_STANDARD_MODEL` / `IMAGE_QUALITY_MODEL` | go-comic-kit Config のモデル指定（Vertex AI 経由、ADC 認証のため API キーは不要） |
| `STYLE_SUFFIX` / `DESIGN_STYLE_SUFFIX` | 画風指定（省略時は kit の既定） |
| `MAX_CHAPTERS` / `MAX_PANELS_PER_CHAPTER` / `MAX_PANELS_PER_PAGE` | go-comic-kit Config の台本・ページ割り制御 |
| `MAX_CONCURRENCY` | 一括生成の並列数（既定 1 = 逐次）。上げる場合は `RATE_INTERVAL` も見直すこと |
| `RATE_INTERVAL` | AI 呼び出しの発射間隔の下限（既定 10s、0 で無制限）。スループット上限は `MAX_CONCURRENCY` ではなく 1/`RATE_INTERVAL` で決まる |
| `REQUEST_TIMEOUT` | 外部 AI 呼び出し1回あたりの上限（既定 5m）。画像生成1枚に数十秒かかるため短くしすぎないこと |
| `PIPELINE_TIMEOUT` | ワーカータスク1件（台本→パネル→ページの工程列全体）の上限（既定 45m、0 以下で無制限）。`REQUEST_TIMEOUT` が1回の API 呼び出しの上限であるのに対し、こちらは列全体を包みます |
| `GCP_PROJECT_ID` / `GCP_LOCATION_ID` | GCP プロジェクトとリージョン |
| `CLOUD_TASKS_QUEUE_ID` | Cloud Tasks キュー名。タスクを投入するのは web 面だけなので `SERVER_ROLE=worker` では不要 |
| `SERVICE_ACCOUNT_EMAIL` | Cloud Tasks の OIDC トークンに**署名する**サービスアカウント。受信側は許可リストとして同じ値を照合するため、**web と worker で必ず同じ値**にする（実行 SA とは別物） |
| `TASK_AUDIENCE_URL` | OIDC トークンの audience。web/worker を分けた場合は**呼び先である worker の URL**（Cloud Run の IAM が不一致を 403 で弾く） |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | ブラウザ Google OAuth ログイン |
| `SESSION_SECRET` / `SESSION_ENCRYPT_KEY` | セッションクッキーの署名鍵・暗号化鍵 |
| `ALLOWED_EMAILS` / `ALLOWED_DOMAINS` | ログインを許可するメール/ドメイン（カンマ区切り、いずれか必須） |

## 🔀 web / worker の分離

本番では 1 つのイメージを 2 つの Cloud Run サービスとしてデプロイし、`SERVER_ROLE` で役割を切り替えます（`cloudbuild.yaml`）。

| | `ap-story`（web） | `ap-story-worker` |
|---|---|---|
| `SERVER_ROLE` | `web` | `worker` |
| 提供するルート | `/api/*`, `/auth/*` | `/tasks/generate` |
| 公開 | あり | **なし**（Cloud Run の IAM で遮断） |
| memory / cpu | 512Mi / 1 | 1Gi / 2 |
| 実行 SA | `ap-story-web-runner` | `ap-story-worker-runner` |
| シークレット | OAuth 4 点 | `SLACK_WEBHOOK_URL` のみ |

`SERVER_ROLE` を未指定にすると両方の面を提供します。ローカル開発（`go run ./main.go`）はこの状態で動きます。

分離する理由は 3 つあります。

1. **デプロイ設定を役割ごとに最適化できる** — 漫画生成は数分〜数十分かかるため worker は長い timeout が要りますが、その上限を Web 面にまで課す必要はありません
2. **ログとメトリクスが役割ごとに読める** — Cloud Run の組み込みメトリクスはサービス単位です
3. **タスク受付口を非公開にできる** — 同居していると `/tasks/generate` が公開サービス上に存在し、防御はアプリ内の OIDC 検証だけになります。分離後は Cloud Run の IAM がコンテナに届く前に弾きます

役割ごとに構築される依存も変わります。`SERVER_ROLE=web` では Cloud Tasks の投入クライアントだけを、`worker` では OAuth ハンドラを構築しません。worker の Cloud Tasks 検証は OAuth 設定を要求しない `auth.TaskVerifier`（gcp-kit v1.6.0 以降）で行うため、OAuth 系シークレットが不要になります。

権限定義は `ap-infra` リポジトリの `app_ap_story.tf` にあります。
| `ALLOWED_M2M_SERVICE_ACCOUNTS` | M2M 呼び出しを許可する SA（カンマ区切り） |
| `SLACK_WEBHOOK_URL` | 完了通知（任意） |

---

### 📜 ライセンス (License)

* このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
