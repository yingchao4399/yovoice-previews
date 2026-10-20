import { test, expect, type Page } from '@playwright/test';
import { emptyState } from '../src/shared/workbench';
import { readFile } from 'node:fs/promises';

const fileName = 'choir.wav';
const storageRoot = 'D:\\音乐工作室\\yovoice-data';

async function openLibrary(page: Page) {
  page.on('pageerror', error => console.error('播放测试页面错误:', error.message));
  const state = emptyState();
  state.preferences = { ...state.preferences, introSeen: true, tourSeen: true };
  state.arrangementMaterials = [{ id: 'choir', projectId: 'project', planHash: 'plan', sectionIndex: 0, phraseIndex: 0, partIndex: 0,
    instrument: '合唱', role: 'pad', technique: '无词哼唱', modelId: 'yue2-3b-q8', fileName, duration: 12, createdAt: '2026-10-20T09:00:00Z' }];
  state.instrumentSources = [{ id: 'xiao', name: '箫', instrument: '箫', origin: 'real', samples: [
    { id: 'xiao-d4', fileName: 'xiao-d4.wav', sha256: 'test', rootPitch: 62, articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 12 },
  ] }];
  await page.addInitScript(({ state, storageRoot }) => {
    const listeners: ((event: MessageEvent) => void)[] = [];
    window.__workbenchMediaBase = '/media/';
    window.chrome = { ...window.chrome, webview: {
      addEventListener: (_name, listener) => { listeners.push(listener); },
      postMessage: (request: unknown) => {
        const { id, method } = request as { id: string; method: string };
        const result = method === 'state.get' ? { state, catalog: [{ id: 'yue2-3b-q8', name: 'YuE2 3B', family: 'yue2', precision: 'Q8', version: '1' }] } : method === 'storage.info' ? { root: storageRoot } : null;
        queueMicrotask(() => listeners.forEach(listener => listener({ data: { id, result } } as MessageEvent)));
      },
    } };
  }, { state, storageRoot });
  // 使用浏览器真实解码的 PCM WAV，不替换 play()。
  const frames = 12 * 8000;
  const wav = Buffer.alloc(44 + frames * 2);
  wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34);
  wav.write('data', 36); wav.writeUInt32LE(frames * 2, 40);
  for (let i = 0; i < frames; i++) wav.writeInt16LE(Math.round(1500 * Math.sin(i * 2 * Math.PI * 220 / 8000)), 44 + i * 2);
  const body = process.env.YOVOICE_TEST_CHOIR_WAV ? await readFile(process.env.YOVOICE_TEST_CHOIR_WAV) : wav;
  await page.route(/\/media\/(outputs|instruments)\/[^/]+\.wav$/, route => {
    const range = /^bytes=(\d+)-(\d*)$/.exec(route.request().headers().range ?? '');
    const start = range ? Number(range[1]) : 0;
    const end = range?.[2] ? Math.min(Number(range[2]), body.length - 1) : body.length - 1;
    return route.fulfill({ status: range ? 206 : 200, body: body.subarray(start, end + 1), contentType: 'audio/wav',
      headers: { 'Accept-Ranges': 'bytes', 'Content-Length': String(end - start + 1), ...(range ? { 'Content-Range': `bytes ${start}-${end}/${body.length}` } : {}) } });
  });
  await page.goto('/');
  await page.getByTestId('nav-instruments').click();
}

test('AI 素材在列表、详情和空搜索切换后仍可播放及拖动', async ({ page }) => {
  await openLibrary(page);
  await page.getByRole('tab', { name: 'AI 音源与模型' }).click();
  await page.getByRole('button', { name: '试听', exact: true }).click();
  await expect.poll(() => page.locator('audio').evaluateAll(elements => elements.some(element => (element as HTMLAudioElement).currentTime > 0))).toBe(true);
  await page.getByRole('button', { name: '查看详情' }).click();
  const pause = page.getByRole('button', { name: '暂停', exact: true });
  await page.getByRole('button', { name: /^(暂停|试听)$/ }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  if (await pause.count()) await pause.click();
  await page.getByRole('button', { name: '试听', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect.poll(() => page.locator('audio').evaluateAll(elements => elements.some(element => !(element as HTMLAudioElement).paused && !!(element as HTMLAudioElement).currentSrc))).toBe(true);
  await page.getByRole('button', { name: '返回音源库' }).click();
  await page.getByRole('textbox', { name: '搜索乐器、奏法或来源' }).fill('不存在');
  await page.getByRole('textbox', { name: '搜索乐器、奏法或来源' }).fill('');
  if (await pause.count()) await pause.click();
  await page.getByRole('button', { name: '试听', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await page.getByRole('slider', { name: '播放进度' }).fill('5');
  await expect.poll(() => page.locator('audio').evaluateAll(elements => elements.some(element => (element as HTMLAudioElement).currentTime >= 5))).toBe(true);
});

test('查看采样和 AI 详情显示当前目录下的完整文件路径', async ({ page }) => {
  await openLibrary(page);
  await page.getByRole('button', { name: '查看采样' }).click();
  await expect(page.getByText(`${storageRoot}\\instruments`, { exact: true })).toBeVisible();
  await expect(page.getByText(`${storageRoot}\\instruments\\xiao-d4.wav`, { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '返回音源库' }).click();
  await page.getByRole('tab', { name: 'AI 音源与模型' }).click();
  await page.getByRole('button', { name: '查看详情' }).click();
  await expect(page.getByText(`${storageRoot}\\outputs\\${fileName}`, { exact: true })).toBeVisible();
});

test('AI 生成素材与模型在独立分类内搜索和显示', async ({ page }) => {
  await openLibrary(page);
  await page.getByRole('tab', { name: 'AI 音源与模型' }).click();
  await expect(page.getByRole('heading', { name: '合唱 · 无词哼唱' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'YuE2 3B' })).toHaveCount(0);
  await page.getByRole('radio', { name: /AI.*模型/ }).click();
  await expect(page.getByRole('heading', { name: 'YuE2 3B' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '合唱 · 无词哼唱' })).toHaveCount(0);
});
