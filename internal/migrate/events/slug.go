package events

import (
	"strings"
	"unicode"
)

// slugify 依前綴與標題產生 slug：保留字母與數字（含中文），去除標點與空白。
func slugify(title, prefix string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
