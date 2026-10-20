import { test, expect } from '@playwright/test';
import { emptyState } from '../src/shared/workbench';
import { createMusicDraft } from '../src/features/music/music-draft';
import { seedState } from './seed';

test('版本快捷切换保留正文，记住选择；历史条目等高且可以全部展开', async ({ page }) => {
  const state = emptyState();
  const draft = { ...createMusicDraft('zh-CN'), id: 'b'.repeat(32), title: '版本对比', text: '当前编辑内容不能被历史覆盖' };
  state.drafts = [draft];
  state.history = [0, 1, 2].map(index => ({
    id: String(index + 1).repeat(32), title: `演奏版本 ${3 - index}`, fileName: `take-${index}.wav`, duration: 30,
    createdAt: new Date(Date.UTC(2026, 9, 20 - index)).toISOString(),
    settings: { ...draft, text: index ? '很长的历史风格说明。'.repeat(40) : '短风格', lyrics: '[Verse]\n' + '演唱的历史歌词\n'.repeat(index * 20) },
  }));
  await seedState(page, state); await page.goto('/');
  const quick = page.getByRole('button', { name: '快速切换版本', exact: true });
  await quick.click();
  await page.getByRole('menuitem', { name: /演奏版本 2/ }).click();
  await expect(page.getByLabel('风格', { exact: true })).toHaveValue(draft.text);
  await expect.poll(() => page.evaluate(id => JSON.parse(localStorage.getItem('yovoice-history-selection') ?? '{}')[id], draft.id)).toBe(state.history[1].id);
  await page.reload(); await quick.click();
  await expect(page.getByRole('menuitem', { name: /演奏版本 2/ }).locator('svg')).toHaveCount(1);
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '历史版本', exact: true }).click();
  const rows = page.locator('.history-item');
  await expect(rows).toHaveCount(3);
  const heights = await rows.evaluateAll(elements => elements.map(element => element.getBoundingClientRect().height));
  expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(1);
  await page.getByRole('button', { name: '全部展开', exact: true }).click();
  await expect(page.locator('.history-details')).toHaveCount(3);
  await page.getByRole('button', { name: '全部收起', exact: true }).click();
  await expect(page.locator('.history-details')).toHaveCount(0);
  await page.getByRole('button', { name: /展开.*演奏版本 2/ }).click();
  await expect(page.locator('.history-details')).toHaveCount(1);
  await expect(page.locator('.history-details')).toContainText('演唱的历史歌词');
});

test('小窗口的音乐正文可以滚到旋律谱，播放器面板可调高度', async ({ page }) => {
  const state = emptyState();
  state.drafts = [{ ...createMusicDraft('zh-CN'), id: 'c'.repeat(32), modelId: 'yue2-3b-q8', text: 'pipa, xiao', lyrics: '[Verse]\n' + '山间听弦\n'.repeat(80) }];
  await seedState(page, state); await page.setViewportSize({ width: 1180, height: 700 }); await page.goto('/');
  const panel = page.locator('#music-compose-panel');
  const melody = page.getByRole('heading', { name: '旋律谱', exact: true });
  await melody.scrollIntoViewIfNeeded();
  await expect(melody).toBeInViewport();
  expect(await panel.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  const handle = page.getByRole('separator', { name: '调整音频区高度' });
  await expect(handle).toBeVisible();
  const player = page.locator('.player-single');
  const before = await player.boundingBox(); const grip = await handle.boundingBox();
  await page.mouse.move(grip!.x + grip!.width / 2, grip!.y + grip!.height / 2);
  await page.mouse.down(); await page.mouse.move(grip!.x + grip!.width / 2, grip!.y - 80, { steps: 8 }); await page.mouse.up();
  expect((await player.boundingBox())!.height).toBeGreaterThan(before!.height);
  await melody.scrollIntoViewIfNeeded(); await expect(melody).toBeInViewport();
});
