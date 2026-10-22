package score

import (
	"fmt"
	"strings"
)

// PhraseVocalABC 将已校验的旋律放在人声声部，避免无词合唱又被转为纯器乐。
func PhraseVocalABC(text string) (string, error) {
	before, err := ParseABC(text)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	for i := 0; i+3 < len(lines); i++ {
		if lines[i] == "V: Vocal" && lines[i+2] == "V: Ins" {
			lines[i+1], lines[i+3] = lines[i+3], lines[i+1]
			i += 3
		}
	}
	text = strings.Join(lines, "\n")
	after, err := ParseABC(text)
	if err != nil {
		return "", err
	}
	if len(after.Vocal().Notes) != len(before.Ins().Notes) {
		return "", fmt.Errorf("人声条件转换不完整")
	}
	return text, nil
}
