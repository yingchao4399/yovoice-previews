package workbench

import "yovoice/internal/schema"

func ptr(s string) *string { return &s }

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// truncateRunes 按字符数截断文本。
func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// scoreLanguage 是界面语言对应的英文语言名，写进提示词让模型用它命名。
func scoreLanguage(locale schema.UiLocale) string {
	if name, ok := schema.UiLocaleNames[locale]; ok {
		return name
	}
	return "English"
}
