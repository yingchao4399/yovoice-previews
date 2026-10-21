package score

import (
	"fmt"
	"strings"
)

// NormalizeKey 只归一明确等价的大小调；民族调式不能擅自降格为大调。
func NormalizeKey(value string) (string, error) {
	s := strings.TrimSpace(value)
	if s == "" {
		return "C", nil
	}
	// 部分旧工程明确写了“D 羽调式（D minor）”。只采用其中已声明、且主音一致的
	// 模型大小调上下文；调用方保留原声明并提示，不能由未知调式自行猜出 C 大调。
	context := strings.NewReplacer("（", "(", "）", ")").Replace(s)
	if at := strings.Index(context, "("); at > 0 && strings.HasSuffix(context, ")") {
		inside := context[at+1 : len(context)-1]
		outside := strings.TrimSpace(context[:at])
		if !strings.ContainsAny(inside, "()") {
			tonic := outside[:1]
			if len(outside) > 1 && (outside[1] == '#' || outside[1] == 'b') {
				tonic = outside[:2]
			}
			if key, err := NormalizeKey(inside); err == nil && strings.EqualFold(tonic, strings.TrimSuffix(key, "m")) {
				return key, nil
			}
		}
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "♯", "#"), "♭", "b")
	if _, ok := abcKeys[s]; ok {
		return s, nil
	}
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	for _, suffix := range []struct{ from, to string }{{"小调", "m"}, {"大调", ""}, {"minor", "m"}, {"major", ""}, {"min", "m"}, {"maj", ""}} {
		if strings.HasSuffix(s, suffix.from) {
			s = strings.TrimSuffix(s, suffix.from) + suffix.to
			break
		}
	}
	if s == "" {
		return "", fmt.Errorf("无法表示调性 %q", value)
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if _, ok := abcKeys[s]; !ok {
		return "", fmt.Errorf("调性 %q 无法无损转换为模型支持的大小调；请明确选择等价调性", value)
	}
	return s, nil
}
