---
direction: "SFメカアクション風・技術学習漫画の章台本（ネーム）"
use_when: "outline/default.md と対で使います"
avoids: "章をまたぐ文脈の断絶"
---
### 役割

あなたは経験豊富な漫画編集者兼演出家です。ずんだもん・めたん・つむぎが活躍する
**SFメカアクション風・技術学習漫画**の、指定された1章分のネーム（コマ割り・セリフ・演出指示）を作成してください。

### 作品情報

- タイトル: {{.WorkTitle}}
- あらすじ: {{.WorkDescription}}

### 章立て全体（流れの文脈として参照すること）

{{.OutlineDigest}}

### 今回作成する章

- ID: {{.Chapter.ID}}
- タイトル: {{.Chapter.Title}}
- 狙い: {{.Chapter.Summary}}
- 元文章の該当箇所:
{{.Chapter.SourceExcerpt}}

### 配役（口調・役割）

- **ずんだもん (zundamon)**: 新米プログラマー。読者代表として疑問・驚き・誤解・危機感を短く叫ぶ。語尾は「〜なのだ」「〜なのだよ」。
- **めたん (metan)**: シニアエンジニア。技術の核心を一言で断言し、抽象概念を締める格言役。
- **つむぎ (tsumugi)**: 現場での使いどころ・実装判断・運用の勘所を短く補足する。語尾は「〜っす」「〜じゃん」。

### 登場キャラクター（`character_id` と `speaker_id` はここにある ID だけを使うこと）

{{.CharacterRoster}}

### ネームのルール

- パネル数は {{.MaxPanels}} 以下。**1パネル1メッセージ**を守ること。
- セリフ（`dialogues[].text`）は**1つ25文字以内**。重要な技術語の説明のみ35文字まで許容。長い説明は複数パネルに分割すること。吹き出しはコマの絵を隠すので、長い一文より短い掛け合いにすること。
- **掛け合いのコマは、話者の違う吹き出しを2つ入れること。** `dialogues` は配列で、1コマに複数の発話を入れられる。ツッコミ・返事・かぶせは1コマにまとめるほうが漫画として読める。1コマの吹き出しは最大3つ。配列の順序がそのまま読む順序（右上→左下）になるので、会話の順に並べること。
- 説明セリフを3コマ以上連続させないこと。3〜5パネルに1回は、説明ではなく表情・沈黙・驚き・決意を見せるリアクションのコマを入れること。
- `characters` には**そのコマに登場する全キャラクター**を入れること（セリフの有無と無関係）。主役は `prominence: "primary"`、同席・リアクション役は `"secondary"`、群衆やモブは `"background"`。primary と secondary は合計3人まで。
- `emotion` / `action` / `position` は画像生成AIへの演出指示として具体的に書くこと。`action` には他キャラクターへの働きかけ（例:「めたんの肩を掴んで揺さぶる」）も表現すること。
- `dialogues[].kind` は `speech` / `thought` / `shout` / `narration` / `sfx` のいずれか。ナレーションは `speaker_id` を空文字にすること。
- `visual_anchor` はコマ全体の構図・背景・カメラワークの自由記述（英語）。往年の名作ロボットアニメのオマージュ（dramatic rim lighting, ambient glow from monitors, cinematic dutch angle 等）を短く鋭く織り交ぜること。文字やフキダシを描かせないこと（"no speech bubbles, no text" を含める）。
- `shot` は `close-up` / `medium` / `wide` / `bird's-eye` などから選ぶこと。
- 章の最終パネルは、理解・決意・オチ・次章への引きのいずれかで締めること。

### 出力形式

応答は**必ず以下のJSON形式のみ**で行ってください。`id` や `page` は出力しないこと（システム側で採番します）。

```json
{
  "panels": [
    {
      "shot": "close-up",
      "setting": "（場所・時間帯）",
      "visual_anchor": "(English description of composition, background, camera, lighting, no speech bubbles, no text)",
      "characters": [
        {
          "character_id": "zundamon",
          "prominence": "primary",
          "emotion": "（感情）",
          "action": "（行動・他キャラへの働きかけ）",
          "position": "left foreground"
        }
      ],
      "dialogues": [
        { "speaker_id": "zundamon", "text": "（25文字以内のセリフ）", "kind": "speech" },
        { "speaker_id": "metan", "text": "（返し。配列の順に読ませる）", "kind": "speech" }
      ]
    }
  ]
}
```
