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

// Delete buttons on the history list and detail pages
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

// Design-sheet delete buttons on the character detail and generation history pages
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

// --- Regeneration (work detail page) ---

// enqueueRegenerate enqueues a regeneration command.
// Generation is asynchronous, so once enqueued the button is stopped and only a notice is shown.
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

// randomSeed makes a new seed for regeneration.
// Without a new seed go-comic-kit generates under the same conditions as before, so the art does not change.
const randomSeed = () => Math.floor(Math.random() * 2147483647) + 1;

// Enqueueing image generation (generating panels and pages, per chapter, and individual regeneration).
// The whole work and a chapter share one handler because what is sent is the same: a command and a target.
document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-regenerate');
    if (!btn) return;

    const { jobId, command, target, mode, stage } = btn.dataset;
    const payload = { command };

    // A page is a composite of the panels, so move on only after checking the panels.
    // Page composition runs when the same command is sent with all panels present (generated ones are skipped).
    if (stage === 'panels') {
        payload.stop_after_panels = true;
    }

    // Commands that make images carry the image model chosen on screen.
    // The initial choice is the value recorded on the work, so as long as people keep pressing, the work stays consistent.
    if (command === 'render_comic' || command === 'regenerate_panel' || command === 'regenerate_page') {
        const model = document.querySelector('.js-image-model')?.value;
        if (model) {
            payload.model_override = model;
        }
    }

    // regenerate_panel targets with panel_id, regenerate_page with page
    if (command === 'regenerate_panel') {
        payload.panel_id = target;
    } else if (command === 'regenerate_page') {
        payload.page = Number(target);
    } else if (command === 'regenerate_chapter_script') {
        payload.chapter_id = target;
    } else if (command === 'render_comic' && target) {
        // "この章の画像を生成" from a chapter card. Without a target it covers the whole work.
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

// --- Reading and proofreading the script (tab on the work detail page) ---
//
// The script is fetched from the API when the tab is opened, not when the page is rendered.
// Merely opening the detail page fetches signed URLs for every panel, so adding the script there
// would make everyone pay the cost every time, even those who never look at it.

// SOFT_LINE_LIMIT is the dialogue length the script prompt asks of the writing
// (assets/prompts/chapter/default.md). Saving still works beyond it, but
// the bubble hides the panel's art, so it is marked to be noticed while reading.
const SOFT_LINE_LIMIT = 25;

const DIALOGUE_KINDS = ['speech', 'shout', 'thought', 'narration', 'sfx'];

window.App.script = {
    // draft is the script fetched from the API. Edits write directly into this object.
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

// renderScript assembles the script in the order chapter → page → panel.
// Page breaks are shown as headings because the page is the unit recomposed when dialogue is fixed.
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

// dialogueRow is the edit row for one speech bubble.
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

// loadScript fetches the script only the first time the tab is opened.
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

// Every input is written back to draft. It is not re-read from the screen on save so that
// rows not on display (edits made in another tab) are not missed.
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

// saveScript saves the script and reports from the result which pages need recomposing.
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

        // Saving is not the goal. Showing it in the art requires page composition.
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
