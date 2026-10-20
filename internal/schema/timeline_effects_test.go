package schema

import "testing"

// 音轨处理参数从前端进入持久化时必须保持在 Web Audio 支持的范围内。
func TestTimelineEffectLimits(t *testing.T) {
	timeline := &AudioTimeline{Tracks: []AudioLane{{ID: "lead", Name: "主奏", Clips: []AudioClip{}, SoftenDB: -6, Compression: .5}}}
	if err := ValidateTimeline(timeline, nil); err != nil {
		t.Fatal(err)
	}
	for _, effect := range []struct{ soften, compression float64 }{{-12.01, .5}, {0, 1.01}} {
		timeline.Tracks[0].SoftenDB, timeline.Tracks[0].Compression = effect.soften, effect.compression
		if err := ValidateTimeline(timeline, nil); err == nil {
			t.Fatalf("越界音轨处理参数被接受：%+v", timeline.Tracks[0])
		}
	}
}
