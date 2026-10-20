import { test, expect } from '@playwright/test';
import { seedState } from './seed';
import { emptyState } from '../src/shared/workbench';

test('音乐作品可在创作、编曲、音频和导出间切换，原歌曲参数保持独立', async ({ page }, testInfo) => {
  await page.goto('/');
  await page.getByTestId('nav-new').click();
  await page.getByRole('menuitem', { name: '音乐生成', exact: true }).click();
  await expect(page.getByLabel('风格', { exact: true })).toHaveValue('');

  await page.getByRole('combobox', { name: '民乐方案' }).click();
  await page.getByRole('option', { name: '江南丝竹' }).click();
  await page.getByRole('button', { name: '应用到风格' }).click();
  await expect(page.getByLabel('风格', { exact: true })).toHaveValue(/Jiangnan sizhu/);
  await page.screenshot({ path: testInfo.outputPath('music-folk-create.png') });

  await page.getByRole('tab', { name: '曲谱编排' }).click();
  await expect(page.getByRole('heading', { name: '编曲', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '填入示例' }).click();
  await expect(page.getByRole('button', { name: '渲染配乐' })).toBeEnabled();
  await page.getByRole('button', { name: '声部表现' }).click();
  await page.getByRole('combobox', { name: '选择声部' }).click();
  await page.getByRole('option', { name: '小提琴' }).click();
  await page.getByRole('button', { name: '自然演奏' }).click();
  await page.screenshot({ path: testInfo.outputPath('music-folk-score.png') });
  await page.getByRole('tab', { name: '音频制作' }).click();
  await page.getByRole('button', { name: '创建多轨时间线' }).click();
  await expect(page.getByText('多轨时间线已在下方打开。')).toBeVisible();
  await expect(page.locator('.multitrack')).toBeVisible();
  await page.getByRole('tab', { name: '曲谱编排' }).click();
  await expect(page.locator('.score-arrangement')).toBeVisible();
  await expect(page.locator('.multitrack')).toBeHidden();
  await page.getByRole('tab', { name: '导出' }).click();
  await expect(page.getByRole('button', { name: '前往导出 MIDI' })).toBeVisible();

  await expect(page.locator('.saved')).toContainText('已保存');
  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts.find((draft: { kind?: string }) => draft.kind === 'music'));
  expect(saved).toMatchObject({ kind: 'music', modelId: 'ace-step-1.5-turbo-bf16', folkProfileId: 'jiangnan-sizhu' });
  expect(saved.score.tracks.length).toBeGreaterThan(0);
  expect(saved.score.tracks.find((track: { id: string }) => track.id === 'violin').humanize).toMatchObject({ velocity: 8, timingMs: 10 });
  expect(saved.timeline).toMatchObject({ tracks: [] });
});

test('同一音乐作品的歌曲与编曲版本可加入多轨时间线', async ({ page }, testInfo) => {
  const state = emptyState(), draft = state.drafts[0];
  draft.kind = 'music'; draft.modelId = 'ace-step-1.5-turbo-bf16'; draft.title = '民乐试作';
  draft.timeline = { tracks: [{ id: 'music-lane', name: '音轨 1', muted: false, clips: [] }] };
  state.history = [
    { id: 'song-version', title: '民乐试作', fileName: 'song.wav', createdAt: '2026-10-17T01:00:00Z', duration: 12, settings: { ...draft } },
    { id: 'score-version', title: '民乐试作', source: 'score', fileName: 'score.wav', createdAt: '2026-10-17T02:00:00Z', duration: 10, settings: { ...draft } },
    { id: 'other-version', title: '其他作品', fileName: 'other.wav', createdAt: '2026-10-17T03:00:00Z', duration: 8, settings: { ...draft, id: 'another-project' } },
  ];
  await seedState(page, state); await page.goto('/');
  await page.getByRole('tab', { name: '音频制作' }).click();
  await page.getByRole('button', { name: '添加音频到音轨 1' }).click();
  await page.getByRole('menuitem', { name: '从历史版本添加' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('button', { name: '歌曲 · 民乐试作' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: '编曲 · 民乐试作' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: '其他作品' })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath('music-audio-history.png') });
  await dialog.getByRole('button', { name: '编曲 · 民乐试作' }).click();
  await expect(page.locator('.multitrack-clip')).toHaveCount(1);
  await expect(page.locator('.saved')).toContainText('已保存');
  await page.reload();
  await page.getByRole('tab', { name: '音频制作' }).click();
  await expect(page.locator('.multitrack-clip')).toHaveCount(1);
  const saved = await page.evaluate(id => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts.find((item: { id: string }) => item.id === id), draft.id);
  expect(saved.timeline.tracks[0].clips[0].generationId).toBe('score-version');
});
