export interface FolkProfile {
  id: string;
  name: Record<'zh-CN' | 'en', string>;
  ensemble: Record<'zh-CN' | 'en', string>;
  prompt: string;
}

// 方案描述聚焦编制与演奏方式，不把地域或戏曲简单当成一个固定音阶。
export const folkProfiles: readonly FolkProfile[] = [
  { id: 'jiangnan-sizhu', name: { 'zh-CN': '江南丝竹', en: 'Jiangnan Sizhu' }, ensemble: { 'zh-CN': '笛、箫、二胡、琵琶、扬琴；小编制与声部呼应', en: 'Dizi, xiao, erhu, pipa and yangqin in a small ensemble' }, prompt: 'Jiangnan sizhu chamber ensemble, dizi and erhu melodic dialogue, pipa and yangqin accompaniment, delicate ornamentation, natural acoustic dynamics, no electronic drums' },
  { id: 'guqin-song', name: { 'zh-CN': '古琴清唱', en: 'Guqin Song' }, ensemble: { 'zh-CN': '古琴主奏、人声清唱、少量箫；留白与呼吸', en: 'Guqin, intimate unprocessed vocal and restrained xiao' }, prompt: 'intimate guqin and natural Chinese vocal, sparse xiao responses, quiet room ambience, expressive pauses and human breath, minimal processing' },
  { id: 'shanbei-folk', name: { 'zh-CN': '陕北民歌', en: 'Northern Shaanxi Folk' }, ensemble: { 'zh-CN': '高亢领唱、三弦或板胡伴奏；宽阔自由的句法', en: 'Open-throated lead vocal with sanxian or banhu' }, prompt: 'northern Shaanxi folk song, open-throated expressive lead vocal, sanxian and banhu accompaniment, free phrasing, acoustic performance, no dance beat' },
  { id: 'kunqu', name: { 'zh-CN': '昆曲配乐', en: 'Kunqu' }, ensemble: { 'zh-CN': '曲笛为主，笙与打击乐点缀；唱腔留足空间', en: 'Qudi lead with sheng and light percussion around the vocal' }, prompt: 'Kunqu inspired traditional theatre accompaniment, qudi leads, restrained sheng and percussion cues, spacious phrasing for operatic vocal, organic acoustic sound' },
  { id: 'yueju', name: { 'zh-CN': '越剧配乐', en: 'Yue Opera' }, ensemble: { 'zh-CN': '主胡与琵琶、扬琴呼应；柔和抒情', en: 'Lead huqin with pipa and yangqin responses' }, prompt: 'Yue opera inspired lyrical accompaniment, expressive huqin lead, pipa and yangqin responses, gentle acoustic dynamics, room for theatrical singing' },
  { id: 'pingtan', name: { 'zh-CN': '苏州评弹', en: 'Suzhou Pingtan' }, ensemble: { 'zh-CN': '琵琶、三弦与说唱；节奏跟随语言', en: 'Pipa and sanxian supporting narrative singing' }, prompt: 'Suzhou pingtan storytelling, pipa and sanxian accompaniment, speech-led rhythm, intimate natural vocal, subtle tempo flexibility, minimal processing' },
  { id: 'jingju', name: { 'zh-CN': '京剧基础配器', en: 'Peking Opera' }, ensemble: { 'zh-CN': '京胡领奏，月琴与锣鼓经配合唱段', en: 'Jinghu lead, yueqin and opera percussion cues' }, prompt: 'Peking opera inspired accompaniment, jinghu lead, yueqin and traditional gong and drum cues, dramatic pauses, space for operatic vocal, acoustic sound' },
] as const;
