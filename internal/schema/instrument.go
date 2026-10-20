package schema

// InstrumentSource 是一件采样乐器的来源清单。文件均保存在托管音源目录中，
// 音高和奏法必须经过核验后才能由乐谱渲染器调用。
type InstrumentSource struct {
	ID                  string             `json:"id"`
	CatalogID           string             `json:"catalogId,omitempty"`
	Name                string             `json:"name"`
	Instrument          string             `json:"instrument"`
	Origin              string             `json:"origin"`                 // real 或 ai
	Verification        string             `json:"verification,omitempty"` // catalog、user 或 metadata；metadata 仅表示名称与单音自动核验，仍可人工试听。
	DefaultArticulation string             `json:"defaultArticulation,omitempty"`
	Author              string             `json:"author,omitempty"`
	License             string             `json:"license,omitempty"`
	LicenseURL          string             `json:"licenseUrl,omitempty"`
	SourceURL           string             `json:"sourceUrl,omitempty"`
	Samples             []InstrumentSample `json:"samples"`
	Plugin              *PluginInstrument  `json:"plugin,omitempty"`
	Provenance          string             `json:"provenance,omitempty"`
	Health              string             `json:"health,omitempty"`
}

// 插件预设归属于库内音源，工程携带状态和依赖，不复制插件二进制。
type PluginInstrument struct {
	Path          string             `json:"path"`
	Validated     bool               `json:"validated,omitempty"`
	Dependencies  []PluginDependency `json:"dependencies,omitempty"`
	Name          string             `json:"name"`
	SHA256        string             `json:"sha256"`
	StateData     []byte             `json:"stateData,omitempty"`
	Articulations []string           `json:"articulations"`
}

type InstrumentSample struct {
	ID           string  `json:"id"`
	FileName     string  `json:"fileName"`
	SHA256       string  `json:"sha256"`
	RootPitch    int     `json:"rootPitch"`
	Articulation string  `json:"articulation"`
	VelocityLow  int     `json:"velocityLow"`
	VelocityHigh int     `json:"velocityHigh"`
	Duration     float64 `json:"duration"`
}

type PluginDependency struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
