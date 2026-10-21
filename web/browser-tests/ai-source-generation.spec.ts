import { test, expect } from '@playwright/test';
import { seedState } from './seed';
import { emptyState } from '../src/shared/workbench';
import { createScoreDraft } from '../src/features/score/score-draft';

test('AI 声部默认按谱采样，二级设置可切换自由乐句并保留曲谱', async ({ page }) => {
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.arrangement = { title: 'AI 生成验证', tempo: 80, timeSignature: [4, 4], seconds: 3, approved: true, sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '独奏', startBar: 1, endBar: 1, parts: [{ instrument: '箫', role: 'melody', source: 'ai' }] }] }] };
  state.drafts = [draft];
  await seedState(page, state); await page.goto('/'); await page.getByTestId('nav-arrangement').click();
  await page.getByRole('button', { name: '生成设置', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '箫 · 生成设置' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('combobox', { name: '创作模式', exact: true })).toContainText('按谱');
  await expect(dialog.getByRole('combobox', { name: '实现方式', exact: true })).toContainText('音色按谱演奏');
  await dialog.getByRole('combobox', { name: '创作模式', exact: true }).click();
  await page.getByRole('option', { name: '自由', exact: true }).click();
  await expect(dialog.getByRole('combobox', { name: '实现方式', exact: true })).toContainText('模型直接生成乐句');
  await expect(dialog.getByRole('combobox', { name: '生成内容', exact: true })).toContainText('独立乐句');
  await dialog.getByRole('button', { name: '保存设置', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect.poll(() => page.evaluate(id => JSON.parse(localStorage.getItem('voice-workbench-v1') ?? '{}').drafts.find((d: { id: string }) => d.id === id)?.arrangement.sections[0].phrases[0].parts[0].generation?.mode, draft.id)).toBe('free');
  const saved = await page.evaluate(id => JSON.parse(localStorage.getItem('voice-workbench-v1') ?? '{}').drafts.find((d: { id: string }) => d.id === id), draft.id);
  expect(saved.score).toEqual(draft.score);
});

test('小窗口生成设置可以滚动，保存操作保持可见', async ({ page }) => {
  await page.setViewportSize({ width: 960, height: 640 });
  const state = emptyState(); const draft = createScoreDraft('zh-CN', false);
  draft.arrangement = { title: '合唱设置', tempo: 80, timeSignature: [4, 4], seconds: 3, approved: true, sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '合唱', startBar: 1, endBar: 1, parts: [{ instrument: '合唱', role: 'pad', source: 'ai' }] }] }] };
  state.drafts = [draft]; await seedState(page, state); await page.goto('/'); await page.getByTestId('nav-arrangement').click();
  await page.getByRole('button', { name: '生成设置', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('button', { name: '保存设置', exact: true })).toBeInViewport();
  await expect(dialog.getByRole('combobox', { name: '生成内容', exact: true })).toContainText('独立乐句');
  await expect(dialog.getByRole('combobox', { name: '创作模式', exact: true })).toContainText('按谱');
  await expect(dialog.getByRole('combobox', { name: '实现方式', exact: true })).toContainText('模型直接生成乐句');
  await dialog.getByRole('combobox', { name: '声音类别', exact: true }).scrollIntoViewIfNeeded();
  await expect(dialog.getByRole('combobox', { name: '声音类别', exact: true })).toContainText('无词合唱 / 人声哼唱');
});
