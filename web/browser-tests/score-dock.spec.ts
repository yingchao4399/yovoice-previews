import { test, expect } from '@playwright/test';
import { seedState } from './seed';
import { emptyState } from '../src/shared/workbench';
import { createScoreDraft } from '../src/features/score/score-draft';

test('从已拼合作品新建编曲不能残留旧曲谱或重复面板', async ({ page }) => {
  test.setTimeout(60_000);
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.title = '暗夜圣咏';
  draft.score = { tempo: 80, timeSignature: [4, 4], key: 'D minor', sections: [], tracks: Array.from({ length: 12 }, (_, index) => ({
    id: `old-${index}`, name: `旧圣咏声部${index}`, program: 48, notes: [{ bar: 1, beat: 1, pitch: 60, velocity: 90, length: 1 }],
  })) };
  draft.timeline = { tracks: [{ id: 'assembled', name: '旧圣咏音轨', muted: false, clips: [] }] };
  state.drafts = [draft];
  await seedState(page, state);
  await page.goto('/');
  await expect(page.locator('.score-track')).toHaveCount(12);
  const dock = page.locator('.score-dock');
  const height = (await dock.boundingBox())!.height;
  const resize = page.getByRole('separator', { name: '调整曲谱区高度' });
  await resize.focus();
  await page.keyboard.press('ArrowUp');
  await expect.poll(async () => (await dock.boundingBox())!.height).toBeGreaterThan(height);
  await page.getByRole('button', { name: '收起曲谱轨道' }).click();
  await expect(page.locator('.score-arrangement')).toHaveCount(0);
  await page.getByRole('button', { name: '展开曲谱轨道' }).click();
  await expect(page.locator('.score-track')).toHaveCount(12);
  await page.getByTestId('nav-new').click();
  await page.getByRole('menuitem', { name: '编曲生成', exact: true }).click();
  await expect(page.locator('.score-summary')).toContainText('0 小节');
  await expect(page.locator('.score-track')).toHaveCount(0);
  await expect(page.locator('.score-arrangement')).toHaveCount(0);
  await expect(page.getByText('旧圣咏音轨', { exact: true })).toHaveCount(0);
});
