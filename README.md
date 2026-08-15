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
* **📝 台本ゲート**: Web フォームは**台本までしか作りません**。章立て・ネームを確認してから
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

ただし `compose_comic` 以外は task name に投入時刻を含めて、重複排除が効く範囲を
「同じ対象へ同じ秒に届いた投入」— つまり呼び出し元の再試行のような、まとめてよい重複だけに
絞っています。`render_comic` は「失敗したところから再開する」ため、`regenerate_*` と
`generate_design_sheet` は「気に入らないからもう一度」のため、いずれも同じ対象へ何度も
投げ直す前提の操作です。対象だけで名前を決めると、その投げ直しが ALREADY_EXISTS として
黙って捨てられます。Cloud Tasks の重複排除は**完了後もしばらく効き続け**（公称1時間、実際は
それより長引きます）、gcp-kit はこれを成功として扱うので、呼び出し元には 202 が返り、
ジョブ状態は誰も処理しないまま `queued` で残ります。エラーが出ないぶん気づきにくく、
実際に同じコマの再生成が数時間にわたって空振りし続けたことがあります。

`compose_comic` だけは投入のたびにジョブ ID を新規採番するため名前がもともと投入ごとに
変わり、決定的な名前が排除できるのは同一リクエストの再送だけなので、時刻を含めていません。

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

### 台本の校正

セリフの手直しは `GET/PUT /api/comics/{jobID}/script` で行います（画面は作品詳細の「台本」タブ）。
台本の再生成（`regenerate_chapter_script`）は章まるごと書き直すので1行の校正には使えず、
コマの構成ごと変わって生成済みのコマ画像との対応が壊れます。

やり取りするのは state 全体ではなく、章の見出しと各コマのセリフだけです。生成記録
（`GenerationRecord`）やページ成果物は生成の結果であって入力ではないので、編集の経路に乗せません。
差し替えられるのはセリフの文面・話者・種別だけで、**コマの追加・削除・並べ替えは拒否します**
（`panel_id` の並びが生成済みのコマ画像との対応そのものだからです）。

**保存はゴールではありません。** セリフはページ合成のときに画像モデルが描き込むので
（`prompts/page.go` の `TEXT_TO_RENDER`）、保存しただけでは絵は古い文字のままです。直した文字を
絵に載せるには対象ページを合成し直す必要があり、そのページ番号を `affected_pages` で返します。
逆にコマ画像は作り直さずに済みます（コマは `No speech bubbles, no text` で生成されているため）。

state の保存は「常に上書き・常に最新」で、go-comic-kit の store は条件付き書き込み
（GCS の `ifGenerationMatch`）の口を持ちません。そのため生成ジョブと編集が重なると、
後から書いたほうが勝って先の変更が痕跡なく消えます。防ぎようがないので、実行中
（queued / running）のジョブがある間は編集を 409 で断ります。状態が記録されていない作品は
素通しします（状態を追えないことを理由に止めると、状態記録より前に作られた作品が
永久に直せなくなるためです）。

## 📋 パイプラインコマンド（Task.command）

Worker が受けるタスクはコマンドで分岐します。各コマンドは「state をロード →
go-comic-kit の操作を実行 → state を保存」の形で、go-comic-kit の冪等な操作と1対1です。

state は工程（台本 → パネル → ページ）の切れ目ごとに保存され、ステップが失敗した場合も
そこまでの成果を保存してから終了します。したがって再実行は `render_comic` により未生成分
だけで済み、失敗したジョブの画像を作り直す必要はありません。

| command | 実行する go-comic-kit 操作 | 入力パラメータ |
|---|---|---|
| `compose_comic` | 全工程: GenerateOutline → 各章 GenerateChapterScript → GenerateAllPanels → ComposeAllPages（デザインシートは含まない。単体生成 `generate_design_sheet` で別途作成）。`stop_after_script` を指定すると台本までで止まる | source_url / source_text, script_mode, style_mode, text_model, image_model, stop_after_script |
| `render_comic` | GenerateAllPanels → ComposeAllPages（生成済みは飛ばす）。台本確認後の続行と、失敗・打ち切りからの再開を兼ねる。`chapter_id` でその章だけ、`stop_after_panels` でコマまでに絞れる（画像はいちばん高価な工程なので、1章・1段階ずつ試してから進める） | job_id, chapter_id（任意）, stop_after_panels（任意） |
| `regenerate_chapter_script` | GenerateChapterScript（1章。後続のパネル・ページは別途再生成） | job_id, chapter_id |
| `generate_design_sheet` | GenerateDesignSheet | job_id（省略時は state なしの単発生成）, character_ids, aspect_ratio, layout, style_mode, model_override, seed |
| `regenerate_panel` | GeneratePanel | job_id, panel_id, seed / edit_prompt / prompt_override |
| `regenerate_page` | ComposePage | job_id, page, seed / edit_prompt |

## 🎨 モードとモデルの選択

生成ジョブには4つの選択が乗ります。フォームの `<select>` と JSON API で同じ一覧を使い、
一覧に無い値は投入時に弾かれます（`GET /api/comic-options` で取得できます）。

| 選択 | 出どころ | 内容 |
|---|---|---|
| 台本モード | `assets/prompts/outline`・`chapter` の `.md` | 章立てとネームの語り口。ファイル名がモード名 |
| 画風モード | `assets/prompts/styles.json` | コマ・ページの画風（`style`）、デザインシート用の画風（`design_style`）、その画風で避けたいもの（`negative`）の3点セット |
| テキストモデル | `GEMINI_MODELS` | 台本を書くモデル |
| 画像モデル | `IMAGE_MODELS` | コマ・ページを描くモデル。**選ぶのは作品詳細**（台本の時点ではコマ数も絵柄も分からないため）。最初の画像生成で作品に記録され、以降は既定になる |

選択は `comic_state.json` に記録され、**あとから走らせるジョブが引き継ぎます**。台本確認後の
`render_comic`、章の台本の作り直し、コマ・ページの再生成はいずれも記録された選択を使うため、
1つの作品が途中から別のモデル・別の画風になることはありません。

画風とネガティブプロンプトを1件にまとめてあるのは、対で決まるからです。モノクロの画風を
選んだときに共通側が `monochrome` を禁止していると、指定同士が正面から衝突します。

## ✍️ 生成フロー（Web UI）

1. `/compose` で原稿を投入する。台本モード・画風モード・モデルを選んで「台本を生成」。
   **このフォームは章立てとネームまでしか作らない。** 押した時点では章立てが未実行で、
   何コマになるか分からないため、画像の開始はコマ数が見えている作品詳細に任せる。
2. `/history/{jobID}` で台本を確認する。問題なければ**画像モデルを選んで「コマを生成」**。作品全体でも、
   章カードから1章だけでも始められる。途中で失敗しても**「続きのコマを生成」**が未生成分だけを埋める。
3. コマが揃うと、同じ場所のボタンが**「ページを合成」**に変わる。ページはコマを並べた合成物なので、
   コマの出来を見てから合成へ進む（崩れたコマから2Kのページを作ると払い直しになる）。
4. 気に入らないコマ・ページは詳細画面から個別に直す。
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
| `GET /compose`, `POST /compose` | セッション | 台本生成フォームの表示・投入（受付画面を返す。台本モード・画風モード・テキストモデルを選択できる。画像モデルは作品詳細で選ぶ） |
| `GET /design-sheets`, `POST /design-sheets` | セッション | デザインシート単体生成フォームの表示・投入（job_id は自動採番。`?character_id=`で事前選択可、画風モード・画像モデル・参照画像URL・見た目特徴の上書きに対応） |
| `GET /characters` | セッション | キャラクター一覧（マスター参照画像のサムネイル付き） |
| `GET /characters/{characterID}` | セッション | キャラクター詳細（上段: サイズ/アスペクト比ごとのマスター参照画像、下段: デザインシート生成履歴の最新12件+削除ボタン。新しい順、合成生成は対象外） |
| `GET /characters/{characterID}/history` | セッション | デザインシート生成履歴の全件表示（新しい順、削除ボタン付き） |
| `GET /history` | セッション | 作品一覧画面（ナビ表記は Works。ページング・削除ボタン） |
| `GET /history/{jobID}` | セッション | 作品詳細画面（章・パネル・ページ・デザインシートの閲覧） |
| `GET /static/*` | なし | CSS/JS 静的アセット |
| `GET /api/comic-options` | セッション or M2M | 生成ジョブに指定できる台本モード・画風モード（用途の説明付き）とモデル一覧（先頭が既定）。投入時の許可リストそのもので、フォームの `<select>` と同じ内容 |
| `POST /api/comics` | セッション or M2M | compose_comic ジョブの投入（jobID を返す。script_mode・style_mode・text_model・image_model は任意で、省略時は既定で埋まる） |
| `GET /api/comics` | セッション or M2M | 履歴一覧（state の列挙、ページング） |
| `GET /api/comics/{jobID}` | セッション or M2M | 詳細（comic_state.json の内容） |
| `GET /api/comics/{jobID}/script` | セッション or M2M | 台本の読み出し（章の見出しと各コマのセリフだけ。生成記録やプロンプトは含まない） |
| `PUT /api/comics/{jobID}/script` | セッション or M2M | 台本の保存（セリフのみ差し替え。合成し直すべきページを `affected_pages` で返す） |
| `POST /api/comics/{jobID}/regenerate` | セッション or M2M | 再生成ジョブの投入（command + パラメータ） |
| `GET /api/comics/{jobID}/images/*` | セッション or M2M | パネル・ページ画像への署名 URL リダイレクト |
| `POST /api/design-sheets` | セッション or M2M | generate_design_sheet ジョブの投入（jobID は自動採番。character_ids は必須、aspect_ratio・layout・style_mode・model_override・reference_url_override・visual_cues_override は任意） |
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
| `SERVER_ROLE` | **必須**。プロセスが担う役割。`web` / `worker` / `both`。未設定と未知の値は起動時エラーです。詳細は「web / worker の分離」を参照 |
| `SERVICE_URL` | 自サービスの**公開** URL。OAuth のリダイレクト先、M2M 認証の audience、Slack 通知リンクの生成元を兼ねるため、worker にも**非公開の worker 自身ではなく web の URL** を設定する |
| `WORKER_URL` | Cloud Tasks が呼び出す Worker エンドポイント（省略時は SERVICE_URL 由来）。web 面のみ使用 |
| `STORY_BUCKET` | 成果物・state の GCS バケット |
| `CHARACTERS_JSON_PATH` | go-character-kit の characters.json（GCS/ローカル、任意。未設定時は go-character-kit 埋め込みの既定キャラクター定義を使用） |
| `GEMINI_MODELS` | 台本生成（章立て・章台本）のモデル。カンマ区切りで先頭が既定、全体がフォームの選択肢と投入時の許可リスト。**web で必須**（worker は読みません。ジョブが自分のモデル名を運びます） |
| `IMAGE_MODELS` | 画像生成（デザインシート・パネル・ページ）のモデル。扱いは `GEMINI_MODELS` と同じで、**web で必須** |
| `IMAGE_ASPECT_RATIO` | パネル・ページ・デザインシート共通の比率（`1:1` / `3:4` / `9:16` / `16:9`）。未設定なら `3:4`。**3つで1つの設定**なのは、揃っていないと参照画像によるブレ抑制が黙って無効になるためです |
| `PANEL_IMAGE_SIZE` / `PAGE_IMAGE_SIZE` | 生成画像の解像度（`1K` / `2K`）。未設定ならパネル 1K・ページ/シート 2K。1コマごとに費用が効くのでデプロイ側で選べます |
| `MAX_CHAPTERS` / `MAX_PANELS_PER_CHAPTER` / `MAX_PANELS_PER_PAGE` | go-comic-kit Config の台本・ページ割り制御 |
| `MAX_CONCURRENCY` | 一括生成の並列数（既定 1 = 逐次）。上げる場合は `RATE_INTERVAL` も見直すこと |
| `RATE_INTERVAL` | AI 呼び出しの発射間隔の下限（既定 10s、0 で無制限）。スループット上限は `MAX_CONCURRENCY` ではなく 1/`RATE_INTERVAL` で決まる |
| `REQUEST_TIMEOUT` | 外部 AI 呼び出し1回あたりの上限（既定 5m）。画像生成1枚に数十秒かかるため短くしすぎないこと |
| `PIPELINE_TIMEOUT` | ワーカータスク1件（台本→パネル→ページの工程列全体）の上限（既定 25m）。`REQUEST_TIMEOUT` が1回の API 呼び出しの上限であるのに対し、こちらは列全体を包みます。**dispatch deadline（30m）以上の値と無制限は worker の起動時に拒否されます** |
| `GCP_PROJECT_ID` / `GCP_LOCATION_ID` | GCP プロジェクトとリージョン |
| `CLOUD_TASKS_QUEUE_ID` | Cloud Tasks キュー名。タスクを投入するのは web 面だけなので `SERVER_ROLE=worker` では不要 |
| `TASK_CALLER_SERVICE_ACCOUNT_EMAIL` | 投入するタスクの `oidcToken.serviceAccountEmail` に指定する caller SA。トークンを生成して付与するのは Cloud Tasks であって、このサービスではありません。投入するのは web 面だけなので `SERVER_ROLE=worker` では不要。未設定時は旧 `SERVICE_ACCOUNT_EMAIL` にフォールバックします（移行用・Terraform 適用後に削除） |
| `ALLOWED_TASK_SERVICE_ACCOUNTS` | worker が**受け付ける** caller SA の許可リスト（カンマ区切り、worker では必須）。web と worker で実行 SA を分けている場合、worker が受け付けるべき発行元は自分自身ではなく **web 側の SA**（`ap-story-web-runner`） |
| `TASK_AUDIENCE_URL` | OIDC トークンの audience。web/worker を分けた場合は**呼び先である worker の URL**（Cloud Run の IAM が不一致を 403 で弾く） |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | ブラウザ Google OAuth ログイン |
| `SESSION_SECRET` / `SESSION_ENCRYPT_KEY` | セッションクッキーの署名鍵・暗号化鍵 |
| `ALLOWED_EMAILS` / `ALLOWED_DOMAINS` | ログインを許可するメール/ドメイン（カンマ区切り、いずれか必須） |
| `ALLOWED_M2M_SERVICE_ACCOUNTS` | M2M 呼び出しを許可する SA（カンマ区切り）。`web` 面では必須で、未設定だと起動に失敗します |
| `SLACK_WEBHOOK_URL` | 完了通知（任意）。worker 面のみ使用 |

## 🔀 web / worker の分離

本番では 1 つのイメージを 2 つの Cloud Run サービスとしてデプロイし、`SERVER_ROLE` で役割を切り替えます（`cloudbuild.yaml`）。

| | `ap-story`（web） | `ap-story-worker` |
|---|---|---|
| `SERVER_ROLE` | `web` | `worker` |
| 提供するルート | `/api/*`, `/auth/*` | `/tasks/generate` |
| 公開 | あり | **なし**（ingress と Cloud Run の IAM で遮断） |
| ingress | `all` | **`internal`**（到達経路を Cloud Tasks に限定） |
| memory / cpu | 512Mi / 1 | 1Gi / 2 |
| concurrency / timeout | 20 / 300s | 10 / 1800s |
| 実行 SA | `ap-story-web-runner` | `ap-story-worker-runner` |
| シークレット | OAuth 4 点 | `SLACK_WEBHOOK_URL` のみ |

worker の実行時間の上限を決める値は 3 つあり、この大小関係を守ります。

```
PIPELINE_TIMEOUT  <  dispatch deadline  <=  Cloud Run の timeout
   25m (アプリ)        30m (タスク)            1800s (サービス)
```

実効上限を決めるのは**いちばん小さい値**です。dispatch deadline だけは未指定でも既定の
10 分が効くため、指定を忘れると Cloud Run の timeout が何であれ 10 分でワーカーが
打ち切られます（値は `internal/config/config.go` の `TaskDispatchDeadline`）。

**この大小関係は起動時に強制されます**（`config.validatePipelineTimeout`）。等号も無制限も
拒否するのは、打ち切りが Cloud Tasks 側から来るとプロセスごと止められ、失敗の記録も部分保存も
Slack 通知も走らないまま、`max_attempts = 1` の `story-queue` は再試行しないため、ジョブが
`running` のまま残るためです。記録・通知・部分保存はいずれも打ち切られた context から
切り離して行っています（`internal/pipeline/runner.go` の `failureReportTimeout` と
`partialSaveTimeout`）。

フリート全体の一覧（5 ワークロード分）と、tf の `precondition` による検査は `ap-infra` の
README「タイムアウトの三段」にあります。

`RATE_INTERVAL` を上げるときはこの 3 段に収まるかを先に確認してください。`compose_comic` は
既定値で最大 84 回の AI 呼び出しになり、`10s` でも下限 14 分です。dispatch deadline の上限が
30 分なので、タイムアウトを伸ばして対処する余地はほとんどありません。

`SERVER_ROLE=both` にすると両方の面を提供します。ローカル開発（`go run ./main.go`）はこの状態で動かします。

`SERVER_ROLE` に既定値は無く、未設定なら起動時に落ちます。未設定を `both` とみなすと、本番の
環境変数が 1 つ欠けただけで公開 web に `/tasks/generate` が復活するためです。

分離する理由は 3 つあります。

1. **デプロイ設定を役割ごとに最適化できる** — 漫画生成は数分〜数十分かかるため worker は長い timeout が要りますが、その上限を Web 面にまで課す必要はありません
2. **ログとメトリクスが役割ごとに読める** — Cloud Run の組み込みメトリクスはサービス単位です
3. **タスク受付口を非公開にできる** — 同居していると `/tasks/generate` が公開サービス上に存在し、防御はアプリ内の OIDC 検証だけになります。分離後は Cloud Run の IAM がコンテナに届く前に弾きます

役割ごとに構築される依存も変わります。

- `SERVER_ROLE=web` — Cloud Tasks の投入クライアントを構築します。go-comic-kit の Operations（Vertex AI クライアント）・Slack Notifier・Worker パイプラインは構築しません。生成を実行するのは worker 面だけなので、`ap-story-web-runner` は `aiplatform.user` も `SLACK_WEBHOOK_URL` へのアクセス権も持ちません
- `SERVER_ROLE=worker` — OAuth ハンドラと投入クライアントを構築しません。Cloud Tasks の検証は OAuth 設定を要求しない `auth.TaskVerifier`（gcp-kit v1.6.0 以降）で行うため、OAuth 系シークレットが不要になります

キャラクター定義（`characters.json`）は一覧・デザインシート画面が使うため、役割によらず読み込みます。

worker が受け付ける caller SA は `ALLOWED_TASK_SERVICE_ACCOUNTS` で指定します。タスクに指定される caller SA は web 側（`ap-story-web-runner`）なので、worker 側に必要なのは「その SA からを受け付ける」という設定です。トークンを生成して付与するのは Cloud Tasks であって、web ではありません。この変数が無かった頃は `SERVICE_ACCOUNT_EMAIL` が兼ねており、worker サービスに自分ではない SA のアドレスを入れる必要がありました（未設定なら今もその挙動にフォールバックします）。

権限定義は `ap-infra` リポジトリの `app_ap_story.tf` にあります。

---

### 📜 ライセンス (License)

* このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
