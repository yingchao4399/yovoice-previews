package score

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// PhraseReleaseABC 在主体音符后明确写出休止，提示模型结束发声而非延长最后一个音。
// 休止只占用已有尾音预算，主体音符、调性和时间位置保持不变。
func PhraseReleaseABC(text string, seconds float64) (string, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 5 {
		return "", fmt.Errorf("谱条件尾音预算无效")
	}
	before, err := ParseABC(text)
	if err != nil {
		return "", err
	}
	// 受限 ABC 仅允许到 1/16 拍号；不足一个单位时不虚构更长休止。
	units := int(math.Floor(seconds * float64(before.Tempo) / 15))
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n"))
	b.WriteByte('\n')
	for units > 0 {
		n := min(units, 16)
		fmt.Fprintf(&b, "V: Vocal\nM:%d/16\nZ|\nV: Ins\nM:%d/16\nZ|\n", n, n)
		units -= n
	}
	result := b.String()
	after, err := ParseABC(result)
	if err != nil {
		return "", err
	}
	if !slices.Equal(before.Vocal().Notes, after.Vocal().Notes) || !slices.Equal(before.Ins().Notes, after.Ins().Notes) || after.Seconds()-before.Seconds() > seconds+1e-6 {
		return "", fmt.Errorf("尾音条件改变了主体音符或超出预算")
	}
	return result, nil
}
