import { test, expect, type Page } from '@playwright/test';
import { emptyState, type ArrangementMaterial, type State } from '../src/shared/workbench';
import { createScoreDraft } from '../src/features/score/score-draft';

async function setup(page: Page, rejectRetry = false) {
  const state = emptyState();
  state.preferences = { ...state.preferences, introSeen: true, tourSeen: true };
  const draft = createScoreDraft('zh-CN', false);
  draft.title = '试听队列工程';
  draft.arrangement = { title: '试听队列', tempo: 80, timeSignature: [4, 4], seconds: 6, approved: true,
    sections: [{ name: 'A', startBar: 1, endBar: 2, phrases: ['一', '二'].map((name, i) => ({ name, startBar: i + 1, endBar: i + 1,
      parts: [{ id: `part-${i}`, instrument: '合唱', role: 'pad', source: 'ai' as const }] })) }] };
  state.drafts = [draft];
  state.activity = { kind: 'generate', projectId: draft.id, status: 'running', code: '@yovoice.activity.generate', params: null, received: 1, total: 2, errorCode: null, errorParams: null };
  await page.addInitScript(({ state, rejectRetry }) => {
    const listeners: ((event: MessageEvent) => void)[] = [];
    const calls: { method: string; data: unknown }[] = [];
    Object.assign(window, { __reviewQueueCalls: calls });
    window.__workbenchMediaBase = '/media/';
    // 原生消息经过序列化传输，每次收到独立快照，不能让测试与 React 共用可变对象。
    const push = () => listeners.forEach(listener => listener({ data: { event: 'state', state: structuredClone(state) } } as MessageEvent));
    window.addEventListener('test-state', event => { Object.assign(state, (event as CustomEvent).detail); push(); });
    window.chrome = { ...window.chrome, webview: {
      addEventListener: (_name, listener) => listeners.push(listener),
      postMessage: (request: unknown) => {
const { id, method, data } = request as { id: string; method: string; data: { id?: string; feedback?: string; sessionId?: string; selected?: number[]; custom?: string; action?: string } };
        calls.push({ method, data });
        let error: { code: string; params: { detail: string } } | undefined;
        if (method === 'arrangement.material.review') {
          const m = state.arrangementMaterials!.find(m => m.id === data.id)!;
          m.reviewStatus = 'confirmed'; m.generationResult!.state = 'confirmed'; push();
        }
        if (method === 'arrangement.material.retry') {
          if (rejectRetry) error = { code: '@yovoice.error.unknown', params: { detail: '需要先修复时长合同' } };
          else { state.arrangementMaterials!.find(m => m.id === data.id)!.retryRequest = { status: 'queued', round: 1, feedback: data.feedback }; push(); }
        }
        if (method === 'assistant.open' && state.assistantSessions) { state.assistantSessions.currentId = data.id!; push(); }
        if (method === 'assistant.answer') {
          const q = state.assistantSessions?.sessions.flatMap(session => session.messages.flatMap(message => message.blocks ?? []).map(block => block.question)).find(q => q?.id === data.id);
          if (q) { q.state = data.action === 'later' ? 'deferred' : 'answered'; q.selected = data.selected; q.custom = data.custom; push(); }
        }
        const result = method === 'state.get' ? { state, catalog: [] } : method === 'arrangement.inspect' ? { ready: false, issues: [], parts: [] } : true;
        queueMicrotask(() => listeners.forEach(listener => listener({ data: structuredClone({ id, result, error }) } as MessageEvent)));
      },
    } };
  }, { state, rejectRetry });
  // 真实解码、播放和拖动 PCM 文件；该用例不判断模型生成质量。
  const frames = 16000;
  const body = Buffer.alloc(44 + frames * 2);
  body.write('RIFF'); body.writeUInt32LE(body.length - 8, 4); body.write('WAVEfmt ', 8);
  body.writeUInt32LE(16, 16); body.writeUInt16LE(1, 20); body.writeUInt16LE(1, 22);
  body.writeUInt32LE(8000, 24); body.writeUInt32LE(16000, 28); body.writeUInt16LE(2, 32); body.writeUInt16LE(16, 34);
  body.write('data', 36); body.writeUInt32LE(frames * 2, 40);
  for (let i = 0; i < frames; i++) body.writeInt16LE(Math.round(1000 * Math.sin(i * Math.PI * 440 / 8000)), 44 + i * 2);
  await page.route('**/media/outputs/*.wav', route => route.fulfill({ body, contentType: 'audio/wav' }));
  await page.goto('/');
  await page.getByRole('button', { name: '试听队列工程', exact: true }).click();
  await page.getByTestId('nav-arrangement').click();
  const candidate = (id: string, phraseIndex: number): ArrangementMaterial => ({ id, projectId: draft.id, partId: `part-${phraseIndex}`, planHash: 'plan', scoreHash: 'score',
    sectionIndex: 0, phraseIndex, partIndex: 0, instrument: '合唱', role: 'pad', phrase: phraseIndex ? '第二句' : '第一句', condition: 'score_conditioned',
    modelId: 'yue2-3b-q8', fileName: `${id}.wav`, duration: 2, createdAt: `2026-10-21T09:00:0${phraseIndex}Z`, reviewStatus: 'needs_review',
    generation: { version: 'ai-source-v3', settings: { assetKind: 'phrase', mode: 'score', method: 'direct', lengthMode: 'phrase', soundType: 'wordless', tailSeconds: .5 }, partId: `part-${phraseIndex}`, scoreHash: 'score', coreSeconds: 1.5, requestSeconds: 2, effectiveOffset: 0, effectiveSeconds: 1.5, tailSeconds: .5, modelId: 'yue2-3b-q8', engineVersion: 'v0.9.1' },
    generationResult: { state: 'needs_review', actualSeconds: 2, truncationKnown: true, attempt: 1, checks: [] } });
  return { state, candidate };
}

async function push(page: Page, state: Partial<State>) {
  await page.evaluate(state => window.dispatchEvent(new CustomEvent('test-state', { detail: state })), state);
}

test('编曲页逐项弹出试听卡，真实播放拖动后确认，后台下一候选继续入队', async ({ page }) => {
  const { candidate } = await setup(page);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const first = candidate('first', 0);
  await push(page, { arrangementMaterials: [first] });
  const dialog = page.getByRole('dialog', { name: /AI 音源试听确认/ });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('第一句');
  await expect(dialog).toContainText('下一项正在生成');
  const adopt = dialog.getByRole('button', { name: '确认采用，继续下一项', exact: true });
  await expect(adopt).toBeDisabled();
  await dialog.getByRole('button', { name: '试听原片', exact: true }).click();
  await expect.poll(() => dialog.locator('audio').evaluateAll(nodes => nodes.some(n => (n as HTMLAudioElement).currentTime > 0))).toBe(true);
  await dialog.getByRole('slider').fill('0.8');
  await expect(adopt).toBeEnabled(); await adopt.click();
  await expect(dialog).toHaveCount(0);
  first.reviewStatus = 'confirmed'; first.generationResult!.state = 'confirmed';
  await push(page, { arrangementMaterials: [first, candidate('second', 1)] });
  await expect(dialog).toContainText('第二句');
  await dialog.getByRole('button', { name: '暂不处理', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByRole('button', { name: '重新打开待试听候选', exact: true }).click();
  await expect(dialog).toContainText('第二句');
});

test('后台生成时自定义重做立即持久排队，切换页面保留，重新生成后再次弹卡', async ({ page }) => {
  const { candidate } = await setup(page);
  const first = candidate('first', 0);
  await push(page, { arrangementMaterials: [first] });
  const dialog = page.getByRole('dialog', { name: /AI 音源试听确认/ });
  await dialog.getByRole('textbox', { name: '自定义修改意见（可选）' }).fill('音色柔和，保持长度');
  await dialog.getByRole('button', { name: '按意见重新生成', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: '1 项重新生成' })).toBeVisible();
  await page.getByTestId('nav-instruments').click();
  await page.getByTestId('nav-arrangement').click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: '1 项重新生成' })).toBeVisible();
  const calls = await page.evaluate(() => (window as unknown as { __reviewQueueCalls: { method: string; data: { feedback?: string } }[] }).__reviewQueueCalls.filter(c => c.method === 'arrangement.material.retry'));
  expect(calls).toHaveLength(1); expect(calls[0].data.feedback).toBe('音色柔和，保持长度');
  first.retryRequest = { status: 'completed', round: 1 };
  first.generationResult!.state = 'rejected';
  const replacement = candidate('regenerated', 0); replacement.createdAt = '2026-10-21T09:00:03Z';
  await push(page, { arrangementMaterials: [first, replacement] });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('audio')).toHaveAttribute('src', /regenerated.wav$/);
  await expect(dialog.getByRole('textbox', { name: '自定义修改意见（可选）' })).toHaveValue('');
});

test('重做被后端拒绝时保留当前试听卡和用户意见', async ({ page }) => {
  const { candidate } = await setup(page, true);
  await push(page, { arrangementMaterials: [candidate('first', 0)] });
  const dialog = page.getByRole('dialog', { name: /AI 音源试听确认/ });
  await dialog.getByRole('textbox', { name: '自定义修改意见（可选）' }).fill('保留自然收尾');
  await dialog.getByRole('button', { name: '按意见重新生成', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('需要先修复时长合同');
  await expect(dialog.getByRole('textbox', { name: '自定义修改意见（可选）' })).toHaveValue('保留自然收尾');
});

test('代表采样确认后等待后台空闲，自动准备该声部剩余采样', async ({ page }) => {
  const { state, candidate } = await setup(page);
  const sample = candidate('sample', 0);
  sample.generation!.pitch = 62;
  sample.generation!.articulation = 'sustain';
  sample.generation!.settings = { ...sample.generation!.settings, method: 'sampler', soundType: 'instrument', assetKind: 'instrument' };
  sample.condition = 'sample_candidate';
  await push(page, { arrangementMaterials: [sample] });
  const dialog = page.getByRole('dialog', { name: /AI 音源试听确认/ });
  await dialog.getByRole('button', { name: '试听原片', exact: true }).click();
  await dialog.getByRole('button', { name: '确认采用，继续下一项', exact: true }).click();
  const preparationCalls = () => page.evaluate(() => (window as unknown as { __reviewQueueCalls: { method: string; data: unknown }[] }).__reviewQueueCalls.filter(c => c.method === 'arrangement.prepare'));
  expect(await preparationCalls()).toHaveLength(0);
  await push(page, { activity: { ...state.activity!, status: 'completed' } });
  await expect.poll(preparationCalls).toEqual([{ method: 'arrangement.prepare', data: { draft: expect.objectContaining({ id: sample.projectId }), mode: 'ai', aiModelId: 'yue2-3b-q8', target: { sectionIndex: 0, phraseIndex: 0, partIndex: 0 } } }]);
});

test('生成失败后的助手问答直接显示在编曲页，明确回答后才续接原诊断会话', async ({ page }) => {
  const { state, candidate } = await setup(page);
  const failed = candidate('failed', 0);
  failed.generationResult = { ...failed.generationResult!, state: 'failed', recoveryQueued: true, recoverySession: 'repair-session', repairRound: 3 };
  await push(page, {
    arrangementMaterials: [failed], activity: { ...state.activity!, status: 'failed' },
    assistantSessions: { currentId: 'other-session', sessions: [{ id: 'other-session', title: '其他对话', updatedAt: new Date().toISOString(), messages: [] }, {
      id: 'repair-session', title: '编曲自动诊断', updatedAt: new Date().toISOString(), messages: [{ id: 'question-message', role: 'assistant', status: 'completed', createdAt: new Date().toISOString(), blocks: [{ kind: 'question', question: {
        id: 'repair-question', sessionId: 'repair-session', turnId: 'repair-turn', workId: failed.projectId, prompt: '三轮修复仍未通过，下一步怎样处理？', options: ['只处理当前声部', '重新规划整首', '暂不重新规划'], state: 'pending',
      } }] }],
    }] },
  });
  const dialog = page.getByRole('dialog', { name: 'AI 生成问题处理', exact: true });
  await expect(dialog).toContainText('三轮修复仍未通过');
  await expect(page.getByRole('dialog', { name: /AI 音源试听确认/ })).toHaveCount(0);
  const calls = () => page.evaluate(() => (window as unknown as { __reviewQueueCalls: { method: string; data: unknown }[] }).__reviewQueueCalls.filter(c => ['assistant.open', 'assistant.answer'].includes(c.method)));
  expect(await calls()).toHaveLength(0);
  await dialog.getByRole('button', { name: '稍后回答', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByRole('button', { name: '重新打开待试听候选', exact: true }).click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: '继续回答', exact: true }).click();
  await dialog.getByRole('button', { name: '只处理当前声部', exact: true }).click();
  await dialog.getByRole('textbox', { name: '自定义回答' }).fill('保留其他已完成音轨');
  await dialog.getByRole('button', { name: '提交回答', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(await calls()).toEqual([
    { method: 'assistant.open', data: { id: 'repair-session' } },
    { method: 'assistant.answer', data: { id: 'repair-question', sessionId: 'repair-session', action: 'later', selected: [], custom: '' } },
    { method: 'assistant.open', data: { id: 'repair-session' } },
    { method: 'assistant.answer', data: { id: 'repair-question', sessionId: 'repair-session', action: 'submit', selected: [0], custom: '保留其他已完成音轨' } },
  ]);
});
