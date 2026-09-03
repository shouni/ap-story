window.App = window.App || {};
window.App.csrfToken = () => document.getElementById('csrf_token')?.value || '';

window.App.deleteResource = async ({
    url,
    confirmMessage,
    failureMessage = '削除に失敗しました。',
    event,
    onSuccess,
    stopPropagation = false
}) => {
    if (event) {
        event.preventDefault();
        if (stopPropagation) event.stopPropagation();
    }

    if (!confirm(confirmMessage)) {
        return false;
    }

    try {
        const response = await fetch(url, {
            method: 'DELETE',
            headers: {
                'X-CSRF-Token': window.App.csrfToken()
            }
        });

        if (!response.ok) {
            const errorText = await response.text();
            alert(errorText ? `${failureMessage}: ${errorText}` : failureMessage);
            return false;
        }

        if (onSuccess) {
            onSuccess();
        }
        return true;
    } catch (error) {
        console.error('Delete Error:', error);
        alert('通信エラーが発生しました。');
        return false;
    }
};

// 履歴一覧・詳細の削除ボタン
document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-delete-history');
    if (!btn) return;

    const jobId = btn.dataset.jobId;
    if (!jobId) return;

    window.App.deleteResource({
        url: `/jobs/${jobId}`,
        confirmMessage: 'この作品の履歴と成果物を削除しますか？',
        event,
        stopPropagation: true,
        onSuccess: () => {
            const item = document.getElementById(`history-item-${jobId}`);
            if (item) {
                item.remove();
            } else {
                window.location.href = '/jobs';
            }
        }
    });
});

// キャラクター詳細・生成履歴のデザインシート削除ボタン
document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-delete-design');
    if (!btn) return;

    const characterId = btn.dataset.characterId;
    const jobId = btn.dataset.jobId;
    if (!characterId || !jobId) return;

    window.App.deleteResource({
        url: `/characters/${characterId}/design-sheets/${jobId}`,
        confirmMessage: 'このデザインシートを削除しますか？',
        event,
        stopPropagation: true,
        onSuccess: () => {
            document.getElementById(`design-item-${jobId}`)?.remove();
        }
    });
});

// --- 再生成（作品詳細） ---

// enqueueRegenerate は再生成系コマンドを投入します。
// 生成は非同期なので、投入できた時点でボタンを止めて案内だけ出します。
window.App.enqueueRegenerate = async ({ jobId, payload, button, pendingLabel = '実行中…' }) => {
    const original = button ? button.innerHTML : null;
    if (button) {
        button.disabled = true;
        button.innerHTML = pendingLabel;
    }

    try {
        const response = await fetch(`/jobs/${jobId}/regenerate`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': window.App.csrfToken()
            },
            body: JSON.stringify(payload)
        });

        if (!response.ok) {
            const errorText = await response.text();
            alert(errorText ? `投入に失敗しました: ${errorText}` : '投入に失敗しました。');
            if (button) {
                button.disabled = false;
                button.innerHTML = original;
            }
            return false;
        }

        if (button) {
            button.innerHTML = '<i class="bi bi-check2 me-1"></i>受付済み';
        }
        return true;
    } catch (error) {
        console.error('Regenerate Error:', error);
        alert('通信エラーが発生しました。');
        if (button) {
            button.disabled = false;
            button.innerHTML = original;
        }
        return false;
    }
};

// randomSeed は再生成用の新しいシードを作ります。
// シードを変えないと go-comic-kit は前回と同じ条件で生成するため、絵が変わりません。
const randomSeed = () => Math.floor(Math.random() * 2147483647) + 1;

// 画像生成の投入（コマ・ページの生成、章単位の生成、個別の再生成）。
// 作品全体と章とで別のハンドラを持たないのは、送るものが command と対象だけで同じだからです。
document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-regenerate');
    if (!btn) return;

    const { jobId, command, target, mode, stage } = btn.dataset;
    const payload = { command };

    // ページはコマを並べた合成物なので、コマの出来を見てから進める。
    // ページ合成はコマが揃った状態で同じコマンドを投げれば走る（生成済みは飛ばされる）。
    if (stage === 'panels') {
        payload.stop_after_panels = true;
    }

    // 画像を作るコマンドには、画面で選んだ画像モデルを添える。
    // 初期選択は作品に記録された値なので、押し続ける限り作品内で揃う。
    if (command === 'render_comic' || command === 'regenerate_panel' || command === 'regenerate_page') {
        const model = document.querySelector('.js-image-model')?.value;
        if (model) {
            payload.model_override = model;
        }
    }

    // regenerate_panel は panel_id、regenerate_page は page で対象を指定する
    if (command === 'regenerate_panel') {
        payload.panel_id = target;
    } else if (command === 'regenerate_page') {
        payload.page = Number(target);
    } else if (command === 'regenerate_chapter_script') {
        payload.chapter_id = target;
    } else if (command === 'render_comic' && target) {
        // 章カードからの「この章の画像を生成」。target が無ければ作品全体になる。
        payload.chapter_id = target;
    }

    if (mode === 'edit') {
        const instruction = prompt('どう直しますか？（例: 表情を笑顔にする）');
        if (!instruction || !instruction.trim()) return;
        payload.edit_prompt = instruction.trim();
    } else if (mode === 'reroll') {
        payload.seed = randomSeed();
    } else if (!confirm(btn.dataset.confirm || '再生成しますか？')) {
        return;
    }

    window.App.enqueueRegenerate({ jobId, payload, button: btn });
});

// --- 台本の閲覧と校正（作品詳細のタブ） ---
//
// 台本は画面のレンダリング時ではなく、タブを開いたときに API から取ります。
// 詳細画面は開くだけで全コマの署名付きURLを引くので、そこへ台本まで載せると
// 見ない人にも毎回コストを払わせることになります。

// SOFT_LINE_LIMIT は台本プロンプトが作文に課しているセリフの長さです
// （assets/prompts/chapter/default.md）。超えていても保存はできますが、
// 吹き出しがコマの絵を隠すので、読みながら気づけるように印を付けます。
const SOFT_LINE_LIMIT = 25;

const DIALOGUE_KINDS = ['speech', 'shout', 'thought', 'narration', 'sfx'];

window.App.script = {
    // draft は API から取った台本です。編集はこのオブジェクトに直接書き込みます。
    draft: null,
    loaded: false
};

const scriptRoot = () => document.getElementById('script-tabs');

const setScriptStatus = (selector, message, isError = false) => {
    const el = document.querySelector(selector);
    if (!el) return;
    el.textContent = message;
    el.classList.toggle('text-danger', isError);
    el.classList.toggle('text-muted', !isError);
};

// renderScript は台本を「章 → ページ → コマ」の順に組み立てます。
// ページの切れ目を見出しで示すのは、セリフを直したときに合成し直す単位がページだからです。
const renderScript = (draft) => {
    const body = document.querySelector('.js-script-body');
    if (!body) return;

    const chapterTitle = new Map((draft.chapters || []).map((c) => [c.chapter_id, c]));
    const parts = [];
    let currentChapter = null;
    let currentPage = null;

    (draft.panels || []).forEach((panel, panelIndex) => {
        if (panel.chapter_id !== currentChapter) {
            currentChapter = panel.chapter_id;
            currentPage = null;
            const chapter = chapterTitle.get(currentChapter);
            parts.push(`<h2 class="h5 mt-4 mb-2 pb-2 border-bottom">${escapeHtml(currentChapter || '')}${
                chapter && chapter.title ? ': ' + escapeHtml(chapter.title) : ''}</h2>`);
            if (chapter && chapter.summary) {
                parts.push(`<p class="text-muted small">${escapeHtml(chapter.summary)}</p>`);
            }
        }
        if (panel.page !== currentPage) {
            currentPage = panel.page;
            parts.push(`<div class="text-muted small mt-3 mb-2">
                <span class="badge text-bg-light border">ページ ${currentPage}</span>
                <span class="ms-1">ここから下を直すと、このページの合成をやり直すことになります</span>
            </div>`);
        }

        parts.push(`<div class="mb-2 ps-2 border-start">
            <div class="d-flex align-items-center gap-2 mb-1">
                <code class="small">${escapeHtml(panel.panel_id)}</code>
                ${panel.shot ? `<span class="badge text-bg-light border text-secondary">${escapeHtml(panel.shot)}</span>` : ''}
            </div>
            ${(panel.dialogues || []).map((line, lineIndex) =>
                dialogueRow(panelIndex, lineIndex, line)).join('')}
        </div>`);
    });

    body.innerHTML = parts.join('');
    document.querySelectorAll('.js-script-save, .js-script-save-json').forEach((b) => { b.disabled = false; });
};

// dialogueRow は1つの吹き出しの編集行です。
const dialogueRow = (panelIndex, lineIndex, line) => {
    const text = line.text || '';
    const count = [...text].length;
    const over = count > SOFT_LINE_LIMIT;
    const kinds = DIALOGUE_KINDS.map((k) =>
        `<option value="${k}"${(line.kind || 'speech') === k ? ' selected' : ''}>${k}</option>`).join('');

    return `<div class="d-flex align-items-center gap-2 mb-1 js-script-line"
                 data-panel-index="${panelIndex}" data-line-index="${lineIndex}">
        <input class="form-control form-control-sm js-line-speaker" style="max-width:9rem"
               value="${escapeHtml(line.speaker_id || '')}" placeholder="話者">
        <select class="form-select form-select-sm js-line-kind" style="max-width:7rem">${kinds}</select>
        <input class="form-control form-control-sm js-line-text" value="${escapeHtml(text)}">
        <span class="js-line-count small text-nowrap ${over ? 'text-danger fw-bold' : 'text-muted'}"
              style="min-width:3rem" title="${SOFT_LINE_LIMIT}文字を超えると吹き出しが絵を隠します">${count}字</span>
    </div>`;
};

const escapeHtml = (value) => String(value ?? '')
    .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;').replaceAll("'", '&#39;');

// loadScript はタブを最初に開いたときだけ台本を取りに行きます。
const loadScript = async () => {
    const root = scriptRoot();
    if (!root || window.App.script.loaded) return;

    try {
        const response = await fetch(`/jobs/${root.dataset.jobId}/script`);
        if (!response.ok) {
            setScriptStatus('.js-script-status', '台本の取得に失敗しました', true);
            return;
        }
        window.App.script.draft = await response.json();
        window.App.script.loaded = true;
        renderScript(window.App.script.draft);
        const json = document.querySelector('.js-script-json');
        if (json) json.value = JSON.stringify(window.App.script.draft, null, 2);
    } catch (error) {
        console.error('Script load error:', error);
        setScriptStatus('.js-script-status', '通信エラーが発生しました', true);
    }
};

document.addEventListener('shown.bs.tab', (event) => {
    if (event.target.classList.contains('js-script-tab')) loadScript();
});

// 入力のたびに draft へ書き戻します。保存時に画面から読み直さないのは、
// 表示していない行（別のタブで編集した内容）を取りこぼさないためです。
document.addEventListener('input', (event) => {
    const row = event.target.closest('.js-script-line');
    if (!row || !window.App.script.draft) return;

    const panel = window.App.script.draft.panels[Number(row.dataset.panelIndex)];
    const line = panel?.dialogues?.[Number(row.dataset.lineIndex)];
    if (!line) return;

    line.speaker_id = row.querySelector('.js-line-speaker').value;
    line.kind = row.querySelector('.js-line-kind').value;
    line.text = row.querySelector('.js-line-text').value;

    const count = [...line.text].length;
    const counter = row.querySelector('.js-line-count');
    counter.textContent = `${count}字`;
    counter.classList.toggle('text-danger', count > SOFT_LINE_LIMIT);
    counter.classList.toggle('fw-bold', count > SOFT_LINE_LIMIT);
    counter.classList.toggle('text-muted', count <= SOFT_LINE_LIMIT);
});

// saveScript は台本を保存し、合成し直すべきページを結果から伝えます。
const saveScript = async (draft, statusSelector, button) => {
    const root = scriptRoot();
    if (!root) return;

    const original = button.innerHTML;
    button.disabled = true;
    setScriptStatus(statusSelector, '保存中…');

    try {
        const response = await fetch(`/jobs/${root.dataset.jobId}/script`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': window.App.csrfToken()
            },
            body: JSON.stringify(draft)
        });
        const payload = await response.json().catch(() => null);

        if (!response.ok) {
            setScriptStatus(statusSelector, payload?.error || '保存に失敗しました', true);
            return;
        }

        window.App.script.draft = payload.script;
        renderScript(payload.script);
        const json = document.querySelector('.js-script-json');
        if (json) json.value = JSON.stringify(payload.script, null, 2);

        // 保存はゴールではありません。絵に反映するにはページ合成が要ります。
        const pages = payload.affected_pages || [];
        setScriptStatus(statusSelector, pages.length
            ? `${payload.changed_lines}行を保存しました。ページ ${pages.join(', ')} を合成し直してください`
            : '変更はありませんでした');
    } catch (error) {
        console.error('Script save error:', error);
        setScriptStatus(statusSelector, '通信エラーが発生しました', true);
    } finally {
        button.disabled = false;
        button.innerHTML = original;
    }
};

document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-script-save');
    if (!btn || !window.App.script.draft) return;
    saveScript(window.App.script.draft, '.js-script-status', btn);
});

document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-script-save-json');
    if (!btn) return;

    const raw = document.querySelector('.js-script-json')?.value || '';
    let parsed;
    try {
        parsed = JSON.parse(raw);
    } catch (error) {
        setScriptStatus('.js-json-status', `JSON として読めません: ${error.message}`, true);
        return;
    }
    saveScript(parsed, '.js-json-status', btn);
});
