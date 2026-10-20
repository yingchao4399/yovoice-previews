import type { Score, UiLocale } from '../../shared/workbench';

// 接收 AI Music Composer 等工具导出的单声部 ABC 基本记谱；不支持的语法明确拒绝，避免静默改错音符。
const pitches: Record<string, number> = { C: 60, D: 62, E: 64, F: 65, G: 67, A: 69, B: 71 };
const signatures: Record<string, number> = {
  C: 0, G: 1, D: 2, A: 3, E: 4, B: 5, 'F#': 6, 'C#': 7,
  F: -1, Bb: -2, Eb: -3, Ab: -4, Db: -5, Gb: -6, Cb: -7,
  Am: 0, Em: 1, Bm: 2, 'F#m': 3, 'C#m': 4, 'G#m': 5, 'D#m': 6, 'A#m': 7,
  Dm: -1, Gm: -2, Cm: -3, Fm: -4, Bbm: -5, Ebm: -6, Abm: -7,
  none: 0,
};
const sharpOrder = 'FCGDAEB', flatOrder = 'BEADGCF';
const invalid = () => new Error('@yovoice.score.abcUnsupported');

function fraction(value: string) {
  const match = /^(\d+)\/(\d+)$/.exec(value);
  if (!match || !Number(match[2])) throw invalid();
  return Number(match[1]) / Number(match[2]);
}

export function abcToScore(source: string, locale: UiLocale): Score & { title?: string } {
  if (source.length > 100000) throw invalid();
  let title = '', meter: [number, number] = [4, 4], noteLength = 1 / 8, tempo = 120, key = 'C';
  const body: string[] = [];
  let started = false;
  for (const raw of source.split(/\r?\n/)) {
    const line = raw.split('%')[0].trim();
    if (!line || /^w:/.test(line)) continue;
    const field = /^([A-Za-z]):\s*(.*)$/.exec(line);
    if (field && !started) {
      const [, name, value] = field;
      if (name === 'T') title = value.slice(0, 120);
      else if (name === 'M') {
        if (value === 'C') meter = [4, 4];
        else if (value === 'C|') meter = [2, 2];
        else { const match = /^(\d+)\/(\d+)$/.exec(value); if (!match) throw invalid(); meter = [Number(match[1]), Number(match[2])]; }
      } else if (name === 'L') noteLength = fraction(value);
      else if (name === 'K') key = value.split(/\s/)[0];
      else if (name === 'Q') {
        const match = /^(?:(\d+\/\d+)=)?(\d+)$/.exec(value);
        if (!match) throw invalid();
        tempo = Number(match[2]) * (match[1] ? fraction(match[1]) * 4 : 1);
      } else if (name === 'V') throw invalid();
      continue;
    }
    if (field) throw invalid();
    started = true; body.push(line);
  }
  if (!body.length || !Number.isInteger(meter[0]) || meter[0] < 1 || meter[0] > 16 || ![2, 4, 8, 16].includes(meter[1]) || noteLength <= 0 || noteLength > 1 || tempo < 30 || tempo > 300 || !(key in signatures)) throw invalid();
  const beatsPerBar = meter[0] * 4 / meter[1];
  const notes: Score['tracks'][number]['notes'] = [];
  const accidentals = new Map<number, number>();
  let position = 0;
  const music = body.join(' ');
  for (let i = 0; i < music.length;) {
    const symbol = music[i];
    if (/\s/.test(symbol)) { i++; continue; }
    if (symbol === '|') { accidentals.clear(); while (music[i] === '|') i++; if (music[i] === ']') i++; continue; }
    if (symbol === '[' && music[i + 1] === '|') { accidentals.clear(); i += 2; continue; }
    let explicit = false, alteration = 0;
    if (symbol === '=' || symbol === '^' || symbol === '_') {
      explicit = true;
      const marker = music[i++];
      if (marker !== '=') { alteration = marker === '^' ? 1 : -1; if (music[i] === marker) { alteration *= 2; i++; } }
    }
    const letter = music[i++];
    if (!letter || !/[A-Ga-gz]/.test(letter) || (explicit && letter === 'z')) throw invalid();
    let octave = letter === letter.toLowerCase() ? 12 : 0;
    while (music[i] === ',' || music[i] === "'") octave += music[i++] === ',' ? -12 : 12;
    const length = /^(\d*)(\/?)(\d*)/.exec(music.slice(i))!;
    i += length[0].length;
    const factor = length[2] ? Number(length[1] || 1) / Number(length[3] || 2) : Number(length[1] || 1);
    const duration = noteLength * 4 * factor;
    if (!Number.isFinite(duration) || duration <= 0 || duration > 64) throw invalid();
    if (!/[zZ]/.test(letter)) {
      const upper = letter.toUpperCase();
      const natural = pitches[upper] + octave;
      if (explicit) accidentals.set(natural, alteration);
      const keyShift = signatures[key] > 0 ? Number(sharpOrder.slice(0, signatures[key]).includes(upper)) : -Number(flatOrder.slice(0, -signatures[key]).includes(upper));
      const pitch = natural + (accidentals.get(natural) ?? keyShift);
      if (pitch < 0 || pitch > 127 || notes.length >= 20000) throw invalid();
      notes.push({ bar: Math.floor(position / beatsPerBar) + 1, beat: position % beatsPerBar + 1, pitch, length: duration, velocity: 80 });
    }
    position += duration;
    if (position / beatsPerBar > 512 || position * 60 / tempo > 600) throw invalid();
  }
  if (!notes.length) throw invalid();
  return { title: title || undefined, tempo, timeSignature: meter, key, tracks: [{ id: 'abc-melody', name: locale === 'zh-CN' ? '旋律' : 'Melody', role: 'melody', program: 0, notes }] };
}
