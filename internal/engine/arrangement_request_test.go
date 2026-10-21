package engine

import (
	"encoding/base64"
	"testing"
	"yovoice/internal/schema"
)

func TestArrangementBudgetIsolatedFromOrdinarySong(t *testing.T) {
	d := schema.Draft{Title: "编曲预算验证", Kind: "music", Text: "solo xiao", SynthesisSettings: schema.SynthesisSettings{ModelID: "yue2-3b-q8", Lyrics: "[Instrumental]"}}
	c := schema.ArrangementGenerationContract{Version: schema.ArrangementGenerationVersion, ModelID: d.ModelID, EngineVersion: "v0.9.1", RequestSeconds: 9, SemanticMin: 200, SemanticMax: 225, Settings: schema.ArrangementGenerationSettings{Mode: "free"}}
	p, err := BuildArrangementRequest(d, c)
	if err != nil {
		t.Fatal(err)
	}
	o := p["request"].(map[string]any)["options"].(map[string]any)
	if o["semantic_max_tokens"] != 225 || o["export_semantic"] != true {
		t.Fatal(o)
	}
	ordinary, err := BuildRequest(d, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ordinary["request"].(map[string]any)["options"].(map[string]any)["semantic_max_tokens"]; ok {
		t.Fatal("编曲预算泄漏至普通歌曲")
	}
	for _, change := range []func(*schema.ArrangementGenerationContract){func(c *schema.ArrangementGenerationContract) { c.SemanticMin = 226 }, func(c *schema.ArrangementGenerationContract) { c.EngineVersion = "v0.7.4" }, func(c *schema.ArrangementGenerationContract) { c.PrefixFrames = 5 }, func(c *schema.ArrangementGenerationContract) { c.Settings.Mode = "score" }} {
		invalid := c
		change(&invalid)
		if _, err := BuildArrangementRequest(d, invalid); err == nil {
			t.Fatal("无效预算或缺失条件被接受", invalid)
		}
	}
}

func TestSemanticTruncationMetadata(t *testing.T) {
	var r taskResult
	r.Artifacts = append(r.Artifacts, struct {
		ID      string            `json:"id"`
		Payload string            `json:"payload"`
		Meta    map[string]string `json:"meta"`
	}{ID: "semantic", Payload: base64.StdEncoding.EncodeToString([]byte("[1,2,3]")), Meta: map[string]string{"truncated": "true"}})
	n, truncated, known := r.semanticResult()
	if n != 3 || !truncated || !known {
		t.Fatal(n, truncated, known)
	}
}
