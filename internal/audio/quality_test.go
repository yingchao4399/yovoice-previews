package audio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/testkit"
)

func TestSignalQualityRejectsSilenceAndClipping(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    []byte
		fail bool
	}{{"静音", testkit.WAV(), true}, {"有效", testkit.WAVSeconds(1), false}, {"削波", func() []byte {
		b := testkit.WAV()
		for i := 44; i < len(b); i += 2 {
			binary.LittleEndian.PutUint16(b[i:], 32767)
		}
		return b
	}(), true}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.wav")
			if e := os.WriteFile(path, tc.b, 0600); e != nil {
				t.Fatal(e)
			}
			_, e := InspectSignal(path)
			if (e != nil) != tc.fail {
				t.Fatalf("质量检查 err=%v", e)
			}
		})
	}
}
