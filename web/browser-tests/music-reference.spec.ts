import { test, expect, type Page } from '@playwright/test';
import { createMusicDraft } from '../src/features/music/music-draft';
import { emptyState } from '../src/shared/workbench';
import { seedState } from './seed';

function audioFixture() {
  const bytes = Buffer.alloc(44 + 48000);
  bytes.write('RIFF'); bytes.writeUInt32LE(bytes.length - 8, 4); bytes.write('WAVEfmt ', 8); bytes.writeUInt32LE(16, 16);
  bytes.writeUInt16LE(1, 20); bytes.writeUInt16LE(1, 22); bytes.writeUInt32LE(24000, 24); bytes.writeUInt32LE(48000, 28);
  bytes.writeUInt16LE(2, 32); bytes.writeUInt16LE(16, 34); bytes.write('data', 36); bytes.writeUInt32LE(48000, 40);
  for (let i = 0; i < 24000; i++) bytes.writeInt16LE(Math.round(Math.sin(i * Math.PI * 2 * 440 / 24000) * 1000), 44 + i * 2);
  return bytes;
}

async function storeAudio(page: Page, file: string) {
  await page.evaluate(async ({ name, bytes }) => {
    await new Promise<void>((resolve, reject) => {
      const request = indexedDB.open('voice-workbench-audio', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('audio');
      request.onerror = () => reject(request.error);
      request.onsuccess = () => {
        const db = request.result, transaction = db.transaction('audio', 'readwrite');
        transaction.objectStore('audio').put(new Blob([new Uint8Array(bytes)], { type: 'audio/wav' }), name);
        transaction.oncomplete = () => { db.close(); resolve(); }; transaction.onerror = () => reject(transaction.error);
      };
    });
  }, { name: file, bytes: [...audioFixture()] });
}

test('参考音色可从声音库搜索试听并复制；参考配乐须显式加入音轨', async ({ page }) => {
  const state = emptyState();
  const draft = { ...createMusicDraft('zh-CN'), id: 'd'.repeat(32), modelId: 'yue2-3b-q8', instrumental: false, text: 'folk', lyrics: '[Verse]\n山间的风' };
  state.drafts = [draft]; state.voices = [{ id: 'a'.repeat(32), name: '清亮参考声', fileName: 'reference.wav', duration: 1 }];
  await seedState(page, state); await page.goto('/'); await storeAudio(page, 'reference.wav');
  await page.getByRole('combobox', { name: '演唱音色', exact: true }).click();
  await page.getByRole('option', { name: '参考音色', exact: true }).click();
  const vocal = page.locator('.music-inspector .music-reference');
  await vocal.getByRole('button', { name: '从库中选择', exact: true }).click();
  const picker = page.getByRole('dialog');
  await picker.getByRole('textbox', { name: '搜索音频或采样' }).fill('清亮');
  await picker.getByRole('button', { name: '试听 · 清亮参考声', exact: true }).click();
  await expect.poll(() => picker.locator('audio').evaluate((audio: HTMLAudioElement) => audio.readyState)).toBeGreaterThan(0);
  await picker.locator('audio').evaluate((audio: HTMLAudioElement) => { audio.currentTime = .5; });
  await expect.poll(() => picker.locator('audio').evaluate((audio: HTMLAudioElement) => audio.currentTime)).toBeGreaterThan(.45);
  await picker.getByRole('button', { name: '使用所选音频', exact: true }).click();
  await expect(picker).toHaveCount(0);
  await vocal.getByRole('textbox', { name: '音色参考描述', exact: true }).fill('清亮、轻柔，保留换气');
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline?.tracks.length)).toBe(0);
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].music.voiceReferenceDescription)).toBe('清亮、轻柔，保留换气');
  await page.getByRole('button', { name: '参考配乐', exact: true }).click();
  const accompaniment = page.locator('.music-editor > .music-reference');
  await accompaniment.locator('input[type=file]').setInputFiles({ name: '箫参考.wav', mimeType: 'audio/wav', buffer: audioFixture() });
  await expect(accompaniment).toContainText('箫参考');
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline.tracks.length)).toBe(0);
  await accompaniment.getByRole('button', { name: '作为素材加入音轨', exact: true }).click();
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline.tracks.length)).toBe(1);
  await accompaniment.getByRole('button', { name: '移除此参考', exact: true }).click();
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline.tracks.length)).toBe(1);
  await page.reload(); await expect(page.locator('.music-inspector .music-reference')).toContainText('清亮参考声');
});

test('逐个采样可搜索并试听，缺文件保持不可选择', async ({ page }) => {
  const state = emptyState(); state.drafts = [{ ...createMusicDraft('zh-CN'), id: 'c'.repeat(32), instrumental: true }];
  state.instrumentSources = [{ id: 'e'.repeat(32), name: '箫真实采样', instrument: '箫', origin: 'real', verification: 'user', license: 'CC0-1.0', samples: [
    { id: 'f'.repeat(32), fileName: 'xiao.wav', sha256: '', rootPitch: 72, articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 },
    { id: 'b'.repeat(32), fileName: 'missing.wav', sha256: '', rootPitch: 74, articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 },
  ] }];
  await seedState(page, state); await page.goto('/'); await storeAudio(page, 'xiao.wav');
  await page.getByRole('button', { name: '参考配乐', exact: true }).click();
  await page.locator('.music-editor .music-reference').getByRole('button', { name: '从库中选择', exact: true }).click();
  const picker = page.getByRole('dialog');
  await picker.getByRole('combobox', { name: '资源类别', exact: true }).click(); await page.getByRole('option', { name: '乐器音源', exact: true }).click();
  await picker.getByRole('button', { name: '查看单个采样', exact: true }).click();
  await expect(picker.getByRole('button', { name: '试听 · 箫真实采样 · MIDI 74 · sustain', exact: true })).toBeDisabled();
  await picker.getByRole('button', { name: '试听 · 箫真实采样 · MIDI 72 · sustain', exact: true }).click();
  await expect.poll(() => picker.locator('audio').evaluate((audio: HTMLAudioElement) => audio.readyState)).toBeGreaterThan(0);
  await picker.getByRole('button', { name: '使用所选音频', exact: true }).click();
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline?.assets?.[0]?.sourceResourceId)).toBe(`sample:${'e'.repeat(32)}/${'f'.repeat(32)}`);
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts[0].timeline.assets[0].origin)).toBe('real');
});
