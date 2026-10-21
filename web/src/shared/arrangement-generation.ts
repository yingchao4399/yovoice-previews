export interface ArrangementGenerationSettings {
  assetKind: 'phrase' | 'instrument'; mode: 'score' | 'free'; method: 'sampler' | 'direct';
  lengthMode: 'phrase' | 'bars' | 'seconds'; bars?: number; seconds?: number;
  soundType: 'instrument' | 'wordless' | 'percussion' | 'effect'; tailSeconds: number; modelId?: string;
}
export interface ArrangementGenerationContract {
  version: string; settings: ArrangementGenerationSettings; partId: string; scoreHash: string;
  coreSeconds: number; requestSeconds: number; effectiveOffset: number; effectiveSeconds: number; tailSeconds: number;
  pitch?: number; velocityLow?: number; velocityHigh?: number; articulation?: string;
  modelId: string; engineVersion: string; semanticMin?: number; semanticMax?: number;
  normalizedKey?: string;
}
export interface ArrangementGenerationResult {
  state: string; actualSeconds: number; semanticFrames?: number; truncated?: boolean; truncationKnown: boolean;
  detectedPitch?: number; checks: { code: string; status: 'pass' | 'fail' | 'review'; message: string }[];
  derivedFile?: string; sourceId?: string; attempt: number;
}
export interface ArrangementGenerationPreflight {
  settings: ArrangementGenerationSettings; contract: ArrangementGenerationContract;
  capability: { modelId: string; family: string; minDuration: number; maxDuration: number; durationGranularity: number; scoreConditioning: boolean; continuation: boolean; isolatedInstrumentReliability: string; evidence: string };
  cells: { key: string; pitch: number; velocityLow: number; velocityHigh: number; articulation: string; sustainSeconds: number; state: string; pilot: boolean; materialId?: string }[];
  reused: number; candidates: number; issues: string[]; warnings?: string[]; estimatedSeconds: number | null;
}

export function generationStateLabel(state: string, zh: boolean): string {
  const labels: Record<string, [string, string]> = { queued: ['排队', 'Queued'], generating: ['生成中', 'Generating'], raw_saved: ['原片已保存', 'Original saved'], checking: ['检查中', 'Checking'], needs_fit: ['待适配', 'Needs fitting'], needs_review: ['待试听', 'Needs audition'], confirmed: ['已确认', 'Confirmed'], ready: ['本句可用', 'Ready'], failed: ['失败', 'Failed'], interrupted: ['已中断', 'Interrupted'], rejected: ['已弃用', 'Rejected'], reused: ['已复用', 'Reused'], missing: ['待补充', 'Missing'] };
  return labels[state]?.[zh ? 0 : 1] ?? state;
}
