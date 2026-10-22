// Package schema 定义持久化的作品、角色、素材与偏好结构，以及与存储无关的校验规则。
package schema

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
	"yovoice/internal/msg"
)

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func ValidID(id string) bool { b, e := hex.DecodeString(id); return e == nil && len(b) == 16 }

type SynthesisSettings struct {
	Music             *MusicCreationOptions     `json:"music,omitempty"`
	ModelOptions      map[string]map[string]any `json:"modelOptions,omitempty"`
	Speaker           string                    `json:"speaker,omitempty"`
	SynthesisLanguage string                    `json:"synthesisLanguage,omitempty"`
	OmniSpeed         float64                   `json:"omniSpeed,omitempty"`
	// 音乐作品中 Text 是风格描述，歌词单独保存；纯音乐时保留歌词但不演唱。
	Lyrics       string `json:"lyrics,omitempty"`
	Instrumental bool   `json:"instrumental,omitempty"`
	// Abc 是 YuE2 的旋律谱（受限 ABC 方言）：有谱时按谱演唱，随版本快照一起保存。
	Abc string `json:"abc,omitempty"`

	VoiceMode         string    `json:"voiceMode"`
	VoxMode           string    `json:"voxMode"`
	VoiceDescription  string    `json:"voiceDescription"`
	ReferenceText     string    `json:"referenceText"`
	GuidanceScale     float64   `json:"guidanceScale"`
	InferenceSteps    int       `json:"inferenceSteps"`
	ModelID           string    `json:"modelId"`
	VoiceID           *string   `json:"voiceId"`
	Mode              string    `json:"mode"`
	EmotionVoiceID    *string   `json:"emotionVoiceId"`
	EmotionText       string    `json:"emotionText"`
	InferEmotion      bool      `json:"inferEmotion"`
	EmotionStrength   float64   `json:"emotionStrength"`
	Emotions          []float64 `json:"emotions"`
	RandomEmotion     bool      `json:"randomEmotion"`
	Language          string    `json:"language"`
	Speed             float64   `json:"speed"`
	Temperature       float64   `json:"temperature"`
	TopP              float64   `json:"topP"`
	TopK              int       `json:"topK"`
	RepetitionPenalty float64   `json:"repetitionPenalty"`
	MaxTokens         int       `json:"maxTokens"`
	IntervalSilenceMs int       `json:"intervalSilenceMs"`
	DoSample          bool      `json:"doSample"`
	NumBeams          int       `json:"numBeams"`
	LengthPenalty     float64   `json:"lengthPenalty"`
	Seed              *int      `json:"seed"`
}

type SubtitleSpeaker struct {
	ID          string             `json:"id"`
	SourceName  string             `json:"sourceName"`
	CharacterID string             `json:"characterId,omitempty"`
	Settings    *SynthesisSettings `json:"settings,omitempty"`
}
type CharacterPerformance struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Settings SynthesisSettings `json:"settings"`
}

type SubtitleCue struct {
	Performance *CharacterPerformance `json:"performance,omitempty"`
	ID          string                `json:"id,omitempty"`
	Start       int64                 `json:"start"`
	End         int64                 `json:"end"`
	Text        string                `json:"text"`
	SpeakerID   string                `json:"speakerId"`
}
type SubtitleDocument struct {
	Speakers []SubtitleSpeaker `json:"speakers"`
	Cues     []SubtitleCue     `json:"cues"`
}
type AudioClip struct {
	GainDB       float64 `json:"gainDb,omitempty"`
	FadeIn       float64 `json:"fadeIn,omitempty"`
	FadeOut      float64 `json:"fadeOut,omitempty"`
	ID           string  `json:"id"`
	GenerationID string  `json:"generationId,omitempty"`
	AssetID      string  `json:"assetId,omitempty"`
	Start        float64 `json:"start"`
	Offset       float64 `json:"offset"`
	Duration     float64 `json:"duration"`
}
type AudioLane struct {
	Origin      string      `json:"origin,omitempty"`
	Solo        bool        `json:"solo,omitempty"`
	Locked      bool        `json:"locked,omitempty"`
	GainDB      float64     `json:"gainDb,omitempty"`
	DuckDB      float64     `json:"duckDb,omitempty"`
	SoftenDB    float64     `json:"softenDb,omitempty"`
	Compression float64     `json:"compression,omitempty"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Muted       bool        `json:"muted"`
	Clips       []AudioClip `json:"clips"`
}
type AudioAsset struct {
	Origin           string  `json:"origin,omitempty"`
	SourceResourceID string  `json:"sourceResourceId,omitempty"`
	License          string  `json:"license,omitempty"`
	LicenseURL       string  `json:"licenseUrl,omitempty"`
	SourceURL        string  `json:"sourceUrl,omitempty"`
	Author           string  `json:"author,omitempty"`
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	FileName         string  `json:"fileName"`
	Duration         float64 `json:"duration"`
}

// ArrangementMaterial 保存 AI 原片和验收记录；只有核验且确认的单音候选可登记为采样，
// 独立乐句不能冒充单音交给采样器。
type ArrangementMaterial struct {
	RetryRequest     *ArrangementMaterialRetry      `json:"retryRequest,omitempty"`
	Generation       *ArrangementGenerationContract `json:"generation,omitempty"`
	GenerationResult *ArrangementGenerationResult   `json:"generationResult,omitempty"`
	ID               string                         `json:"id"`
	ProjectID        string                         `json:"projectId"`
	PlanHash         string                         `json:"planHash"`
	ScoreHash        string                         `json:"scoreHash,omitempty"`
	PartID           string                         `json:"partId,omitempty"`
	Condition        string                         `json:"condition,omitempty"`
	ReviewStatus     string                         `json:"reviewStatus,omitempty"`
	Peak             float64                        `json:"peak,omitempty"`
	ClippedFraction  float64                        `json:"clippedFraction,omitempty"`
	OnsetSeconds     float64                        `json:"onsetSeconds,omitempty"`
	AlignmentWarning string                         `json:"alignmentWarning,omitempty"`
	SectionIndex     int                            `json:"sectionIndex"`
	PhraseIndex      int                            `json:"phraseIndex"`
	PartIndex        int                            `json:"partIndex"`
	Instrument       string                         `json:"instrument"`
	Role             string                         `json:"role"`
	Technique        string                         `json:"technique,omitempty"`
	Phrase           string                         `json:"phrase"`
	Prompt           string                         `json:"prompt"`
	UserFeedback     string                         `json:"userFeedback,omitempty"`
	Tempo            float64                        `json:"tempo"`
	Key              string                         `json:"key,omitempty"`
	ModelID          string                         `json:"modelId"`
	FileName         string                         `json:"fileName"`
	Duration         float64                        `json:"duration"`
	CreatedAt        time.Time                      `json:"createdAt"`
}
type BandPart struct {
	Instrument        string `json:"instrument"`
	Role              string `json:"role"`
	Technique         string `json:"technique,omitempty"`
	Source            string `json:"source"`
	SourceID          string `json:"sourceId,omitempty"`
	AllowSubstitution bool   `json:"allowSubstitution,omitempty"`
}
type BandPreset struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Parts     []BandPart `json:"parts"`
	AIModelID string     `json:"aiModelId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}
type AudioMarker struct {
	ID   string  `json:"id"`
	Time float64 `json:"time"`
	Name string  `json:"name"`
}
type AudioTimeline struct {
	Markers             []AudioMarker `json:"markers,omitempty"`
	RegenerateMode      string        `json:"regenerateMode,omitempty"`
	Assets              []AudioAsset  `json:"assets,omitempty"`
	Tracks              []AudioLane   `json:"tracks"`
	AcceptedGenerations []string      `json:"acceptedGenerations,omitempty"`
}
type Draft struct {
	Performance *CharacterPerformance `json:"performance,omitempty"`
	SynthesisSettings
	Kind        string            `json:"kind,omitempty"`
	CharacterID string            `json:"characterId,omitempty"`
	CreatedAt   *time.Time        `json:"createdAt,omitempty"`
	UpdatedAt   *time.Time        `json:"updatedAt,omitempty"`
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Text        string            `json:"text"`
	Subtitles   *SubtitleDocument `json:"subtitles,omitempty"`
	Timeline    *AudioTimeline    `json:"timeline,omitempty"`
	// Score 仅编曲作品使用，ModelID 指向渲染所用的音色库。
	Score *Score `json:"score,omitempty"`
	// Arrangement 保存智能编曲的分段配器方案，不改变旧编曲作品的渲染路径。
	Arrangement         *ArrangementPlan         `json:"arrangement,omitempty"`
	ArrangementInput    *ArrangementInput        `json:"arrangementInput,omitempty"`
	ArrangementBindings []ArrangementBindingEdit `json:"arrangementBindings,omitempty"`
	// 音乐作品的伴奏音色库与歌曲生成模型分别保存。
	SoundFontID   string `json:"soundFontId,omitempty"`
	FolkProfileID string `json:"folkProfileId,omitempty"`
}

func DefaultDraft() Draft {
	return Draft{ID: NewID(), Title: "清晨旁白", Text: "清晨的阳光穿过窗帘，\n房间渐渐明亮起来。\n\n给自己倒一杯热茶，\n让思绪在片刻的安静里慢下来。\n\n今天，我们从一个好声音开始。", SynthesisSettings: SynthesisSettings{ModelID: "index-2.5-q8", Mode: "text", EmotionText: "温柔、平静，像是在和熟悉的人说话。", EmotionStrength: .6, Emotions: []float64{0, 0, 0, 0, 0, 0, 0, .5}, Language: "zh", Speed: 1, Temperature: .8, TopP: .8, TopK: 30, RepetitionPenalty: 10, MaxTokens: 1500, IntervalSilenceMs: 200, DoSample: true, NumBeams: 3}}
}

type CharacterPreview struct {
	ID       string            `json:"id"`
	FileName string            `json:"fileName"`
	Duration float64           `json:"duration"`
	Settings SynthesisSettings `json:"settings"`
	Text     string            `json:"text"`
}

type Character struct {
	Performances []CharacterPerformance `json:"performances,omitempty"`
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Settings     SynthesisSettings      `json:"settings"`
	DemoText     string                 `json:"demoText"`
	Preview      *CharacterPreview      `json:"preview,omitempty"`
	CreatedAt    time.Time              `json:"createdAt"`
	UpdatedAt    time.Time              `json:"updatedAt"`
}

type Voice struct {
	ReferenceText      string  `json:"referenceText,omitempty"`
	Source             string  `json:"source,omitempty"`
	SourceGenerationID string  `json:"sourceGenerationId,omitempty"`
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	FileName           string  `json:"fileName"`
	Duration           float64 `json:"duration"`
}
type InstalledModel struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Managed bool   `json:"managed"`
}
type Generation struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Source    string             `json:"source,omitempty"`
	FileName  string             `json:"fileName"`
	CreatedAt time.Time          `json:"createdAt"`
	Duration  float64            `json:"duration"`
	Settings  Draft              `json:"settings"`
	Segment   *GenerationSegment `json:"segment,omitempty"`
	// LyricTimes 是播放页校准的歌词时间：按顺序记录每一行（含器乐段落）的开始秒数。
	LyricTimes []float64 `json:"lyricTimes,omitempty"`
	// LyricWords 是强制对齐得到的逐字时间，按歌词顺序排列；与 LyricTimes 只保留较新的一份。
	LyricWords []LyricWord `json:"lyricWords,omitempty"`
}

// LyricWord 是对齐到音频上的一个字（拉丁文为一个词），时间以秒计。
type LyricWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// GenerationSegment 保留单句来源；剪辑与原音频通过生成版本关联。
type GenerationSegment struct {
	Placement    string `json:"placement,omitempty"`
	CueID        string `json:"cueId"`
	SpeakerID    string `json:"speakerId"`
	SpeakerName  string `json:"speakerName"`
	BatchID      string `json:"batchId"`
	Index        int    `json:"index"`
	TargetClipID string `json:"targetClipId,omitempty"`
}
type Activity struct {
	CueID       string `json:"cueId,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	CharacterID string `json:"characterId,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
	Kind        string `json:"kind"`
	// Task 区分同一类操作里的具体任务：melody 为出谱或扒谱，界面据此在旋律谱区域显示进度；align 为歌词对齐，在播放页显示。
	Task        string     `json:"task,omitempty"`
	Code        msg.Code   `json:"code"`
	Params      msg.Params `json:"params"`
	Status      string     `json:"status"`
	Received    int64      `json:"received"`
	Total       int64      `json:"total"`
	ErrorCode   *msg.Code  `json:"errorCode"`
	ErrorParams msg.Params `json:"errorParams"`
	ModelID     *string    `json:"modelId"`
	StartedAt   time.Time  `json:"startedAt"`
}

// UiLocale is the persisted interface language. Distinct from Draft.Language (TTS).
type UiLocale string

const (
	UiLocaleZhCN UiLocale = "zh-CN"
	UiLocaleEn   UiLocale = "en"
)

// UiLocaleNames 列出支持的界面语言及其英文名（写进 AI 提示词），与 web/src/shared/i18n/locale.ts 一致。
var UiLocaleNames = map[UiLocale]string{
	UiLocaleZhCN: "Simplified Chinese",
	"zh-TW":      "Traditional Chinese",
	UiLocaleEn:   "English",
	"ja":         "Japanese",
	"ko":         "Korean",
	"es":         "Spanish",
	"fr":         "French",
	"de":         "German",
	"pt-BR":      "Brazilian Portuguese",
	"ru":         "Russian",
	"it":         "Italian",
	"vi":         "Vietnamese",
}

func ParseUiLocale(raw string) (UiLocale, error) {
	if _, ok := UiLocaleNames[UiLocale(raw)]; !ok {
		return "", fmt.Errorf("unsupported uiLocale %q", raw)
	}
	return UiLocale(raw), nil
}

type Preferences struct {
	ProxyURL       string   `json:"proxyURL,omitempty"`
	ProxyEnabled   *bool    `json:"proxyEnabled,omitempty"`
	DownloadSource string   `json:"downloadSource"`
	Backend        string   `json:"backend"`
	ModelDirectory *string  `json:"modelDirectory"`
	UiLocale       UiLocale `json:"uiLocale"`
	// AITimeout 是单次 AI 请求的超时秒数，0 表示默认值。
	AITimeout int `json:"aiTimeout,omitempty"`
	// IntroSeen 表示已看过首次启动的开场画面，之后启动只显示简短的加载动画。
	IntroSeen bool `json:"introSeen,omitempty"`
	// TourSeen 表示已看过或跳过首次进入主界面的功能引导。
	TourSeen bool `json:"tourSeen,omitempty"`
	// AssistantQuiet 关闭助手的主动提示（生成完成、失败等时刻在角落出现的气泡）。
	AssistantQuiet bool `json:"assistantQuiet,omitempty"`
	// HiddenFeatures 是在设置中隐藏的功能入口（见 OptionalFeatures），为空时全部显示。
	HiddenFeatures []string `json:"hiddenFeatures,omitempty"`
}

// OptionalFeatures 是可在设置中隐藏的功能：故事配音、音乐生成（含编曲）与 Ocho 助手。
var OptionalFeatures = []string{"story", "music", "assistant"}

type State struct {
	Characters           []Character              `json:"characters"`
	Previews             []CharacterPreview       `json:"previews"`
	Drafts               []Draft                  `json:"drafts"`
	Voices               []Voice                  `json:"voices"`
	Models               []InstalledModel         `json:"models"`
	History              []Generation             `json:"history"`
	InstrumentSources    []InstrumentSource       `json:"instrumentSources,omitempty"`
	ArrangementMaterials []ArrangementMaterial    `json:"arrangementMaterials,omitempty"`
	BandPresets          []BandPreset             `json:"bandPresets,omitempty"`
	ArrangementScores    []ArrangementScorePhrase `json:"arrangementScores,omitempty"`
	ArrangementJobs      []ArrangementJob         `json:"arrangementJobs,omitempty"`
	Preferences          Preferences              `json:"preferences"`
	RuntimePath          *string                  `json:"runtimePath"`
	RuntimeBackend       *string                  `json:"runtimeBackend"`
	RuntimeVersion       *string                  `json:"runtimeVersion"`
	// Runtimes 按后端记录下载安装的内核，切换计算设备后再切回时直接复用。
	Runtimes map[string]InstalledRuntime `json:"runtimes,omitempty"`
	Activity *Activity                   `json:"activity"`
	// ModelActivity 是模型下载或导入：不占用推理引擎，与 Activity 中的生成等操作同时进行。
	ModelActivity *Activity `json:"modelActivity"`
	// AIProviders 为可选的大模型服务，AIProviderID 指向当前使用的服务。
	AIProviders  []AIProvider `json:"aiProviders,omitempty"`
	AIProviderID string       `json:"aiProviderID,omitempty"`
	AITask       *AITask      `json:"aiTask,omitempty"`
	// Melody 是最近一次出谱或扒谱的结果，等界面填入作品；只属于本次会话。
	Melody *MelodyResult `json:"melody,omitempty"`
	// AssistantSessions 是助手会话的持久化记录；AssistantLive 只随状态广播（进行中的一轮与界面事件）。
	AssistantSessions *AssistantSessions `json:"assistantSessions,omitempty"`
	AssistantLive     *AssistantLive     `json:"assistantLive,omitempty"`
	// AssistantMemory 是助手记住的用户偏好，所有会话共用。
	AssistantMemory []AssistantMemo `json:"assistantMemory,omitempty"`
}

// MelodyResult 是引擎写出的旋律谱：Source 为 plan（YuE2 按歌词出谱）或 cover（从歌曲扒谱）。
type MelodyResult struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Source    string    `json:"source"`
	Abc       string    `json:"abc"`
	Truncated bool      `json:"truncated,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type InstalledRuntime struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func DefaultState() State {
	return State{Characters: []Character{}, Previews: []CharacterPreview{}, Drafts: []Draft{DefaultDraft()}, Voices: []Voice{}, Models: []InstalledModel{}, History: []Generation{}, Preferences: Preferences{DownloadSource: "modelscope", Backend: "cpu"}}
}
