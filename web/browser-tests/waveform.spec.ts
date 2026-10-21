import { test, expect } from '@playwright/test';
import { parseWavHeader, resamplePeaks, waveform } from '../src/features/media/waveform';

// 不打开页面，校验波形峰值按块读取 WAV、不整段解码（#92：超长音频启动时拖死界面）。

function wavHeader(channels: number, sampleRate: number, bits: number, frames: number, format = 1): Uint8Array {
  const align = channels * bits / 8, bytes = new Uint8Array(44), view = new DataView(bytes.buffer);
  bytes.set(new TextEncoder().encode('RIFF'), 0); view.setUint32(4, 36 + frames * align, true); bytes.set(new TextEncoder().encode('WAVEfmt '), 8);
  view.setUint32(16, 16, true); view.setUint16(20, format, true); view.setUint16(22, channels, true); view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * align, true); view.setUint16(32, align, true); view.setUint16(34, bits, true);
  bytes.set(new TextEncoder().encode('data'), 36); view.setUint32(40, frames * align, true);
  return bytes;
}

const originalFetch = globalThis.fetch;
test.afterEach(() => { globalThis.fetch = originalFetch; });

// 把字节流按小块交给 fetch，模拟边下载边读取；read 记录实际读出的字节数。
function serve(chunks: () => Generator<Uint8Array>, length?: number) {
  const stats = { read: 0 };
  globalThis.fetch = async () => {
    const source = chunks();
    const body = new ReadableStream<Uint8Array>({ pull(controller) { const next = source.next(); if (next.done) controller.close(); else { stats.read += next.value.length; controller.enqueue(next.value); } } });
    return new Response(body, { headers: length ? { 'content-length': String(length) } : {} });
  };
  return stats;
}

test('16 位立体声 WAV 按段取第一声道峰值，块边界不错位', async () => {
  const frames = 1000, pcm = new Uint8Array(frames * 4), view = new DataView(pcm.buffer);
  // 第一声道前一半 0.5、后一半 0.25，第二声道始终满幅，结果不应受第二声道影响。
  for (let i = 0; i < frames; i++) { view.setInt16(i * 4, i < 500 ? 16384 : -8192, true); view.setInt16(i * 4 + 2, 32767, true); }
  const file = new Uint8Array([...wavHeader(2, 8000, 16, frames), ...pcm]);
  serve(function* () { for (let i = 0; i < file.length; i += 7) yield file.slice(i, i + 7); });
  const result = await waveform('memory://a.wav', 4);
  expect(result.duration).toBeCloseTo(frames / 8000);
  expect(result.peaks).toEqual([0.5, 0.5, 0.25, 0.25]);
});

test('超长音频边读取边计算，不需要整段解码', async () => {
  // 1275 秒 48kHz 立体声 16 位，约 245MB，对应 issue 里卡死启动的音频。
  const frames = 1275 * 48000, chunk = new Uint8Array(1 << 16);
  new DataView(chunk.buffer).setInt16(0, 16384, true);
  const header = wavHeader(2, 48000, 16, frames), total = header.length + frames * 4;
  const stats = serve(function* () { yield header; for (let sent = 0; sent < frames * 4; sent += chunk.length) yield chunk.slice(0, Math.min(chunk.length, frames * 4 - sent)); }, total);
  const started = performance.now();
  const result = await waveform('memory://long.wav', 3200);
  expect(result.duration).toBeCloseTo(1275);
  expect(result.peaks).toHaveLength(3200);
  expect(stats.read).toBe(total);
  expect(performance.now() - started).toBeLessThan(15000);
});

test('文件头：浮点与 24 位可读，非 WAV 与不支持的编码交给其他分支', () => {
  expect(parseWavHeader(wavHeader(1, 24000, 32, 10, 3))).toMatchObject({ encoding: 'float', bits: 32, dataOffset: 44, dataSize: 40 });
  expect(parseWavHeader(wavHeader(1, 24000, 24, 10))).toMatchObject({ encoding: 'int', bits: 24, align: 3 });
  expect(parseWavHeader(wavHeader(1, 24000, 16, 10).slice(0, 30))).toBe('more');
  expect(parseWavHeader(new TextEncoder().encode('ID3\u0004 not a wav file'))).toBeNull();
  expect(parseWavHeader(wavHeader(1, 24000, 16, 10, 2))).toBeNull();
});

test('合并峰值时每段取最大值', () => {
  expect(resamplePeaks([0.1, 0.9, 0.2, 0.3, 0.8, 0.1], 3)).toEqual([0.9, 0.3, 0.8]);
  expect(resamplePeaks([0.1, 0.2], 4)).toEqual([0.1, 0.2]);
});
