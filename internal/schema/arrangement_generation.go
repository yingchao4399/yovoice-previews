package schema

const ArrangementGenerationVersion = "ai-source-v3"

// ArrangementGenerationSettings 只用于编曲备料，不改变普通歌曲的推理设置。
type ArrangementGenerationSettings struct {
	AssetKind   string  `json:"assetKind"`  // phrase、instrument
	Mode        string  `json:"mode"`       // score、free
	Method      string  `json:"method"`     // sampler、direct
	LengthMode  string  `json:"lengthMode"` // phrase、bars、seconds
	Bars        int     `json:"bars,omitempty"`
	Seconds     float64 `json:"seconds,omitempty"`
	SoundType   string  `json:"soundType"` // instrument、wordless、percussion、effect
	TailSeconds float64 `json:"tailSeconds"`
	ModelID     string  `json:"modelId,omitempty"`
}

// ArrangementGenerationContract 保存一次生成的不可变条件；有效区段可单独适配。
type ArrangementGenerationContract struct {
	Version              string                        `json:"version"`
	Settings             ArrangementGenerationSettings `json:"settings"`
	PartID               string                        `json:"partId"`
	ScoreHash            string                        `json:"scoreHash"`
	CoreSeconds          float64                       `json:"coreSeconds"`
	RequestSeconds       float64                       `json:"requestSeconds"`
	TailSeconds          float64                       `json:"tailSeconds"`
	EffectiveOffset      float64                       `json:"effectiveOffset"`
	EffectiveSeconds     float64                       `json:"effectiveSeconds"`
	ExpectedOnsetSeconds *float64                      `json:"expectedOnsetSeconds,omitempty"`
	Pitch                *int                          `json:"pitch,omitempty"`
	VelocityLow          int                           `json:"velocityLow,omitempty"`
	VelocityHigh         int                           `json:"velocityHigh,omitempty"`
	Articulation         string                        `json:"articulation,omitempty"`
	ModelID              string                        `json:"modelId"`
	ModelRevision        string                        `json:"modelRevision,omitempty"`
	ModelDigest          string                        `json:"modelDigest,omitempty"`
	EngineVersion        string                        `json:"engineVersion"`
	Seed                 *int                          `json:"seed,omitempty"`
	SemanticMin          int                           `json:"semanticMin,omitempty"`
	SemanticMax          int                           `json:"semanticMax,omitempty"`
	PrefixFrames         int                           `json:"prefixFrames,omitempty"`
	OriginalKey          string                        `json:"originalKey,omitempty"`
	NormalizedKey        string                        `json:"normalizedKey,omitempty"`
	SourceFingerprint    string                        `json:"sourceFingerprint,omitempty"`
	TargetSourceID       string                        `json:"targetSourceId,omitempty"`
}

// ArrangementGenerationResult 不把“文件存在”和“素材可用”混为一谈。
type ArrangementGenerationResult struct {
	RawSHA256       string            `json:"rawSha256,omitempty"`
	DerivedSHA256   string            `json:"derivedSha256,omitempty"`
	State           string            `json:"state"`
	ActualSeconds   float64           `json:"actualSeconds"`
	SemanticFrames  int               `json:"semanticFrames,omitempty"`
	Truncated       bool              `json:"truncated,omitempty"`
	TruncationKnown bool              `json:"truncationKnown"`
	DetectedPitch   *int              `json:"detectedPitch,omitempty"`
	Checks          []GenerationCheck `json:"checks"`
	Request         map[string]any    `json:"request,omitempty"`
	DerivedFile     string            `json:"derivedFile,omitempty"`
	SourceID        string            `json:"sourceId,omitempty"`
	Attempt         int               `json:"attempt"`
	RepairRound     int               `json:"repairRound,omitempty"`
	RecoverySession string            `json:"recoverySession,omitempty"`
	RecoveryQueued  bool              `json:"recoveryQueued,omitempty"`
	RecoveryError   string            `json:"recoveryError,omitempty"`
}

type GenerationCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"` // pass、fail、review
	Message string `json:"message"`
}

// ArrangementMaterialRetry 保存试听后的局部重做请求，切换页面不丢失。
type ArrangementMaterialRetry struct {
	Status   string `json:"status"` // queued、running、completed、failed
	Feedback string `json:"feedback,omitempty"`
	Round    int    `json:"round"`
	Error    string `json:"error,omitempty"`
}

func (m ArrangementMaterial) GenerationResultState() string {
	if m.GenerationResult == nil {
		return "legacy"
	}
	return m.GenerationResult.State
}
