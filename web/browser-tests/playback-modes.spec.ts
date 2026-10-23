import { test, expect, type Page, type Locator } from '@playwright/test';
import { emptyState, type State } from '../src/shared/workbench';
import { createMusicDraft } from '../src/features/music/music-draft';
import { createScoreDraft, exampleScore } from '../src/features/score/score-draft';
import { seedState } from './seed';
import { wavBytes } from './synthetic-song';

async function loadAudio(page: Page, state: State) {
  await page.goto('/');
  const signal = Float32Array.from({ length: 32000 }, (_, i) => .1 * Math.sin(i * 2 * Math.PI * 220 / 8000));
  await page.evaluate(async ({ files, encoded }) => {
    const bytes = Uint8Array.from(atob(encoded), c => c.charCodeAt(0));
    await new Promise<void>((resolve, reject) => {
      const request = indexedDB.open('voice-workbench-audio', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('audio');
      request.onerror = () => reject(request.error);
      request.onsuccess = () => {
        const db = request.result, tx = db.transaction('audio', 'readwrite');
        for (const file of files) tx.objectStore('audio').put(new Blob([bytes], { type: 'audio/wav' }), file);
        tx.oncomplete = () => { db.close(); resolve(); };
        tx.onerror = () => reject(tx.error);
      };
    });
    localStorage.setItem('yovoice-playback-mode', 'sequential');
  }, { files: state.history.map(item => item.fileName), encoded: Buffer.from(wavBytes(signal, signal, 8000)).toString('base64') });
  await seedState(page, state);
  await page.reload();
}

function musicState() {
  const state = emptyState(), draft = { ...createMusicDraft('zh-CN'), id: 'd'.repeat(32), title: '播放回归' };
  state.drafts = [draft];
  state.history = [1, 2].map(n => ({ id: `version-${n}`, title: draft.title, fileName: `version-${n}.wav`, createdAt: `2026-10-23T0${n}:00:00Z`, duration: 4, settings: { ...draft } }));
  return state;
}

async function mode(page: Page, scope: Locator, label: string) {
  await scope.getByRole('combobox', { name: '播放方式', exact: true }).click();
  await page.getByRole('option', { name: label, exact: true }).click();
}

test('资源单条音频可循环，停止后不自动重启', async ({ page }) => {
  const state = musicState();
  await loadAudio(page, state);
  await page.getByTestId('nav-settings').click();
  await page.getByRole('tab', { name: '资源', exact: true }).click();
  const assets = page.getByTestId('assets-page');
  await assets.locator('tbody tr').first().getByRole('button', { name: '试听 播放回归', exact: true }).click();
  const player = assets.locator('.player'), audio = player.locator('audio');
  await mode(page, player, '循环播放');
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.loop)).toBe(true);
  await audio.evaluate((el: HTMLAudioElement) => { el.currentTime = 3.8; });
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.currentTime < 1 && !el.paused)).toBe(true);
  await player.getByRole('button', { name: '暂停', exact: true }).click();
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.paused)).toBe(true);
});

test('波形播放器顺序和随机换曲从头自动播放', async ({ page }) => {
  await loadAudio(page, musicState());
  const player = page.locator('.player:visible'), audio = player.locator('audio');
  await player.getByRole('button', { name: '播放', exact: true }).click();
  const first = await audio.getAttribute('src');
  await audio.evaluate((el: HTMLAudioElement) => { el.currentTime = 3.8; });
  await expect(audio).not.toHaveAttribute('src', first!);
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => !el.paused && el.currentTime < 1.5)).toBe(true);
  await mode(page, player, '随机播放');
  const second = await audio.getAttribute('src');
  await audio.evaluate((el: HTMLAudioElement) => { el.currentTime = 3.8; });
  await expect(audio).not.toHaveAttribute('src', second!);
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => !el.paused && el.currentTime < 1.5)).toBe(true);
});

test('播放页自动换曲不沿用上一首结束位置或暂停状态', async ({ page }) => {
  await loadAudio(page, musicState());
  await page.locator('.player .transport').getByRole('button', { name: '播放页', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '播放回归 · 播放页' }), audio = dialog.locator('audio');
  await dialog.getByRole('button', { name: '版本 2', exact: true }).click();
  await page.getByRole('menuitem', { name: /版本 1/ }).click();
  await audio.evaluate(async (el: HTMLAudioElement) => { await el.play(); el.currentTime = 3.8; });
  await expect(dialog.getByRole('button', { name: '版本 2', exact: true })).toBeVisible();
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => !el.paused && el.currentTime < 1.5)).toBe(true);
  await mode(page, dialog, '循环播放');
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.loop)).toBe(true);
});

test('多轨时间线提供播放方式并循环整段，暂停终止循环', async ({ page }) => {
  const state = musicState();
  state.drafts[0].timeline = { tracks: [{ id: 'lane', name: '主奏', muted: false, clips: [{ id: 'clip', generationId: 'version-1', start: 0, offset: 0, duration: .5 }] }] };
  await loadAudio(page, state);
  await page.getByRole('tab', { name: '音频制作', exact: true }).click();
  const timeline = page.locator('.multitrack');
  await mode(page, timeline, '循环播放');
  await page.evaluate(() => {
    const start = AudioBufferSourceNode.prototype.start;
    Object.assign(window, { scheduledPlaybackStarts: 0 });
    AudioBufferSourceNode.prototype.start = function(...args) {
      (window as unknown as { scheduledPlaybackStarts: number }).scheduledPlaybackStarts++;
      return start.apply(this, args);
    };
  });
  await timeline.getByRole('button', { name: '播放', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { scheduledPlaybackStarts: number }).scheduledPlaybackStarts)).toBeGreaterThan(1);
  await timeline.getByRole('button', { name: '暂停', exact: true }).click();
  await expect(timeline.getByRole('button', { name: '播放', exact: true })).toBeVisible();
  const stopped = await page.evaluate(() => (window as unknown as { scheduledPlaybackStarts: number }).scheduledPlaybackStarts);
  await page.waitForTimeout(650);
  expect(await page.evaluate(() => (window as unknown as { scheduledPlaybackStarts: number }).scheduledPlaybackStarts)).toBe(stopped);
});

test('音轨和编曲指针随实际音频推进，点标尺可跳转', async ({ page }) => {
  const state = emptyState(), draft = { ...createScoreDraft('zh-CN'), id: 's'.repeat(32), score: exampleScore('zh-CN') };
  state.drafts = [draft];
  state.history = [{ id: 'score-audio', title: draft.title, source: 'score', fileName: 'score.wav', createdAt: '2026-10-23T00:00:00Z', duration: 4, settings: { ...draft } }];
  await loadAudio(page, state);
  const player = page.locator('.player:visible'), audio = player.locator('audio');
  await player.getByRole('button', { name: '播放', exact: true }).click();
  await expect.poll(() => player.locator('.playhead').evaluate(el => parseFloat((el as HTMLElement).style.left))).toBeGreaterThan(0);
  await audio.evaluate((el: HTMLAudioElement) => { el.currentTime = 2; });
  await expect.poll(() => player.locator('.playhead').evaluate(el => parseFloat((el as HTMLElement).style.left))).toBeGreaterThan(40);
  await expect.poll(() => page.locator('.score-playhead').getAttribute('style')).toMatch(/0\.[5-9]/);
  const ruler = page.locator('.score-ruler');
  const box = (await ruler.boundingBox())!;
  await ruler.click({ position: { x: box.width / 4, y: box.height / 2 } });
  await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.currentTime)).toBeLessThan(2);
});
