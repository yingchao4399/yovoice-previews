import { test, expect } from '@playwright/test';
import { emptyState } from '../src/shared/workbench';
import { seedState } from './seed';

test('Ocho 问答卡支持点选、自定义、稍后回答和重开，不会当作下载授权', async ({ page }) => {
  const state = emptyState();
  state.aiProviders = [{ id: 'test-provider', preset: 'ollama', name: '本地测试', format: 'openai', baseURL: 'http://localhost:11434/v1', modelsPath: '/models', model: 'qwen3' }];
  state.aiProviderID = 'test-provider';
  state.assistantSessions = { currentId: 'session-one', sessions: [{ id: 'session-one', title: '编曲问答', updatedAt: new Date().toISOString(), messages: [{ id: 'question-message', role: 'assistant', status: 'completed', createdAt: new Date().toISOString(), blocks: [{ kind: 'question', question: {
    id: 'question-one', sessionId: 'session-one', turnId: 'turn-one', prompt: '这段希望怎样演奏？', options: ['琵琶独奏', '琵琶和箫'], state: 'pending',
  } }] }] }] };
  await seedState(page, state); await page.goto('/');
  await page.getByTestId('assistant-toggle').click();
  const card = page.getByTestId('assistant-question');
  await expect(card).toContainText('这段希望怎样演奏？');
  await expect(card.getByRole('button', { name: '提交回答' })).toBeDisabled();
  await card.getByRole('button', { name: '稍后回答' }).click();
  await expect(card.getByRole('button', { name: '继续回答' })).toBeVisible();
  await page.reload();
  await expect(page.getByTestId('assistant-question').getByRole('button', { name: '继续回答' })).toBeVisible();
  await card.getByRole('button', { name: '继续回答' }).click();
  await card.getByRole('button', { name: '琵琶独奏', exact: true }).click();
  await expect(card.getByRole('button', { name: '琵琶独奏', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await card.getByRole('textbox', { name: '自定义回答' }).fill('轮指轻一点，节奏自然');
  await card.getByRole('button', { name: '提交回答' }).click();
  await expect(card).toContainText('琵琶独奏；轮指轻一点，节奏自然');
  await expect(card.getByRole('button', { name: '提交回答' })).toHaveCount(0);
  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!));
  expect(saved.assistantSessions.sessions[0].messages[0].blocks[0].question.state).toBe('answered');
  expect(saved.instrumentSources ?? []).toHaveLength(0);
});
