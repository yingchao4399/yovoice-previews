import { CallError } from './call-error';

// 解码一段音频：每次使用独立的音频上下文，结束后关闭；指定 sampleRate 时按该采样率重采样（人声分析只需 16 kHz）。
export async function decodeAudio(data: ArrayBuffer | Blob, sampleRate?: number): Promise<AudioBuffer> {
  const context = new AudioContext(sampleRate ? { sampleRate } : undefined);
  try { return await context.decodeAudioData(data instanceof Blob ? await data.arrayBuffer() : data); }
  finally { await context.close(); }
}

// 第一声道分成 bins 段的最大振幅（0~1），只取前 seconds 秒；每段最多取 256 个采样，长音频也只做固定的计算量。
export function bufferPeaks(buffer: AudioBuffer, bins: number, seconds = buffer.duration): number[] {
  const data = buffer.getChannelData(0), length = Math.min(data.length, Math.floor(buffer.sampleRate * seconds));
  return Array.from({ length: bins }, (_, i) => {
    const start = Math.floor(i * length / bins), end = Math.floor((i + 1) * length / bins), skip = Math.max(1, Math.floor((end - start) / 256));
    let peak = 0; for (let j = start; j < end; j += skip) peak = Math.max(peak, Math.abs(data[j]));
    return peak;
  });
}

export function encodeWav(buffer: AudioBuffer, start = 0, end = buffer.duration): Blob {
  const first = Math.max(0, Math.floor(start * buffer.sampleRate));
  const last = Math.min(buffer.length, Math.floor(end * buffer.sampleRate));
  const frames = last - first;
  if (frames <= 0) throw new CallError('@yovoice.error.cropRangeInvalid');
  const bytes = new ArrayBuffer(44 + frames * 2); const view = new DataView(bytes);
  const text = (offset: number, value: string) => [...value].forEach((char, i) => view.setUint8(offset + i, char.charCodeAt(0)));
  text(0, 'RIFF'); view.setUint32(4, 36 + frames * 2, true); text(8, 'WAVE'); text(12, 'fmt ');
  view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true);
  view.setUint32(24, buffer.sampleRate, true); view.setUint32(28, buffer.sampleRate * 2, true);
  view.setUint16(32, 2, true); view.setUint16(34, 16, true); text(36, 'data'); view.setUint32(40, frames * 2, true);
  const channels = Array.from({ length: buffer.numberOfChannels }, (_, i) => buffer.getChannelData(i));
  for (let i = 0; i < frames; i++) {
    const sample = Math.max(-1, Math.min(1, channels.reduce((sum, channel) => sum + channel[i + first], 0) / channels.length));
    view.setInt16(44 + i * 2, sample * (sample < 0 ? 32768 : 32767), true);
  }
  return new Blob([bytes], { type: 'audio/wav' });
}
export async function toBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve((reader.result as string).split(',')[1]); reader.onerror = () => reject(reader.error); reader.readAsDataURL(blob); });
}
