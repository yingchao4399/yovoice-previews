import { test, expect } from '@playwright/test';
import { emptyState } from '../src/shared/workbench';
import { seedState } from './seed';

test('多轨横纵滚动和手型平移不修改音块，退出手型后仍可选择音块', async ({ page }) => {
  const state = emptyState();
  const timeline = { tracks: Array.from({ length: 12 }, (_, i) => ({ id: `track-${i}`, name: `乐器 ${i}`, muted: false, clips: [{ id: `clip-${i}`, assetId: 'reference', start: i * 10, offset: 0, duration: 2 }] })), assets: [{ id: 'reference', name: '示例录音', fileName: 'reference.wav', duration: 2 }] };
  state.drafts[0].timeline = timeline;
  await seedState(page, state); await page.goto('/');
  const viewport = page.locator('.multitrack-scroll');
  await expect(viewport).toBeVisible();
  const metrics = await viewport.evaluate(element => ({ width: element.clientWidth, height: element.clientHeight, scrollWidth: element.scrollWidth, scrollHeight: element.scrollHeight,
    parents: [element.parentElement, element.parentElement?.parentElement, element.parentElement?.parentElement?.parentElement].map(p => p && ({ tag: p.tagName, class: p.className, height: p.clientHeight, style: p.getAttribute('style'), display: getComputedStyle(p).display })),
    flex: getComputedStyle(element).flex, minHeight: getComputedStyle(element).minHeight, overflow: getComputedStyle(element).overflow }));
  expect(metrics.scrollWidth, JSON.stringify(metrics)).toBeGreaterThan(metrics.width);
  expect(metrics.scrollHeight, JSON.stringify(metrics)).toBeGreaterThan(metrics.height);
  const box = (await viewport.boundingBox())!;
  await page.mouse.move(box.x + box.width * .7, box.y + box.height * .7);
  await page.keyboard.down('Shift'); await page.mouse.wheel(0, 320); await page.keyboard.up('Shift');
  await expect.poll(() => viewport.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
  await page.mouse.wheel(0, 300);
  await expect.poll(() => viewport.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  await page.getByRole('button', { name: '平移画布', exact: true }).click();
  const before = await viewport.evaluate(element => ({ x: element.scrollLeft, y: element.scrollTop }));
  await page.mouse.move(box.x + box.width * .7, box.y + box.height * .7);
  await page.mouse.down(); await page.mouse.move(box.x + box.width * .5, box.y + box.height * .4, { steps: 6 }); await page.mouse.up();
  await expect.poll(() => viewport.evaluate(element => element.scrollLeft)).toBeGreaterThan(before.x);
  const stored = await page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline);
  expect(stored.tracks).toEqual(timeline.tracks);
  await page.getByRole('button', { name: '平移画布', exact: true }).click();
  await viewport.evaluate(element => { element.scrollLeft = 0; element.scrollTop = 0; });
  const clip = page.locator('.multitrack-clip').first();
  await clip.click();
  await expect(clip).toHaveAttribute('aria-pressed', 'true');
});
