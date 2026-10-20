import type { BandPreset } from './workbench';

// 界面与助手使用相同的资源身份，不能把文件名当成资源 ID。
export interface ResourceItem {
  id: string; kind: string; name: string; status: string; issue?: string; usage?: string;
  parentId?: string; sourceId?: string; sampleId?: string; catalogId?: string; instrument?: string;
  origin?: string; mediaKind?: 'voices' | 'outputs' | 'instruments'; fileName?: string; duration?: number;
  rootPitch?: number; articulation?: string; velocityLow?: number; velocityHigh?: number; sha256?: string;
  license?: string; licenseUrl?: string; sourceUrl?: string; author?: string; bytes?: number; manifest?: string;
  memberIds?: string[]; bandParts?: BandPreset['parts']; aiModelId?: string;
}
export interface ResourceQuery { query?: string; kind?: string; origin?: string; parentId?: string; offset?: number; limit?: number }
export interface ResourcePage { items: ResourceItem[]; total: number; offset: number; nextOffset?: number }
