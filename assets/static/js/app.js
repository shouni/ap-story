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
        url: `/api/comics/${jobId}`,
        confirmMessage: 'この作品の履歴と成果物を削除しますか？',
        event,
        stopPropagation: true,
        onSuccess: () => {
            const item = document.getElementById(`history-item-${jobId}`);
            if (item) {
                item.remove();
            } else {
                window.location.href = '/history';
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
        url: `/api/characters/${characterId}/images/${jobId}`,
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
        const response = await fetch(`/api/comics/${jobId}/regenerate`, {
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

// 画像生成へ進む / 続きを生成（render_comic）
document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-render-comic');
    if (!btn) return;

    if (!confirm('未生成のコマとページの画像生成を開始しますか？')) return;
    window.App.enqueueRegenerate({
        jobId: btn.dataset.jobId,
        payload: { command: 'render_comic' },
        button: btn,
        pendingLabel: '<i class="bi bi-hourglass-split me-1"></i>投入中…'
    });
});

// コマ・ページの再生成（シード振り直し / 指示による編集）
document.addEventListener('click', (event) => {
    const btn = event.target.closest('.js-regenerate');
    if (!btn) return;

    const { jobId, command, target, mode } = btn.dataset;
    const payload = { command };

    // regenerate_panel は panel_id、regenerate_page は page で対象を指定する
    if (command === 'regenerate_panel') {
        payload.panel_id = target;
    } else if (command === 'regenerate_page') {
        payload.page = Number(target);
    } else if (command === 'regenerate_chapter_script') {
        payload.chapter_id = target;
    }

    if (mode === 'edit') {
        const instruction = prompt('どう直しますか？（例: 表情を笑顔にする）');
        if (!instruction || !instruction.trim()) return;
        payload.edit_prompt = instruction.trim();
    } else if (mode === 'reroll') {
        payload.seed = randomSeed();
    } else if (!confirm('再生成しますか？')) {
        return;
    }

    window.App.enqueueRegenerate({ jobId, payload, button: btn });
});
