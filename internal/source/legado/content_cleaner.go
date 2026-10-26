package legado

import (
	"html"
	"regexp"
	"strings"
)

var (
	brRegex      = regexp.MustCompile(`(?i)<br\s*/?>`)
	pRegex       = regexp.MustCompile(`(?i)</p>`)
	divRegex     = regexp.MustCompile(`(?i)</div>`)
	tagRegex     = regexp.MustCompile(`<[^>]+>`)
	extraSpaceRe = regexp.MustCompile(`[\t\f\r ]+`)
	adKeywordsRe = regexp.MustCompile(`(?i)(未完待续|最新网址|收藏本站|记住网址|本章未完|点击下一页|推荐票|月票|笔趣阁|加入书签|手机版阅读)`)
)

// CleanAndFormatContent 遵从 Legado 规范的正文深度净化与排版引擎
func CleanAndFormatContent(rawHTML string, chapterTitle string, replaceRegex string) string {
	if rawHTML == "" {
		return ""
	}

	text := rawHTML

	// 1. 结构换行转换
	text = brRegex.ReplaceAllString(text, "\n")
	text = pRegex.ReplaceAllString(text, "\n")
	text = divRegex.ReplaceAllString(text, "\n")

	// 2. 剥除所有剩余 HTML 标签
	text = tagRegex.ReplaceAllString(text, "")

	// 3. HTML 实体解码
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ") // non-breaking space
	text = strings.ReplaceAll(text, "\u3000", " ") // full-width space

	// 4. 应用书源自定义 replaceRegex 规则 (形如 "##pattern##replacement")
	if replaceRegex != "" {
		text = applyCustomReplaceRegex(text, replaceRegex)
	}

	// 5. 段落拆分与净化排版
	lines := strings.Split(text, "\n")
	var formattedParagraphs []string

	titleClean := strings.TrimSpace(chapterTitle)
	skippedTitle := false

	for _, line := range lines {
		trimmed := extraSpaceRe.ReplaceAllString(strings.TrimSpace(line), " ")
		if trimmed == "" {
			continue
		}

		// 剥离正文首部与章节名重复的冗余行
		if !skippedTitle && titleClean != "" {
			if trimmed == titleClean || strings.HasPrefix(trimmed, titleClean) {
				skippedTitle = true
				continue
			}
			skippedTitle = true
		}

		// 过滤常见单行短广告/水印
		if len([]rune(trimmed)) < 40 && adKeywordsRe.MatchString(trimmed) {
			continue
		}

		// 规范段落首行缩进：两个全角空格（中文小说排版黄金标准）
		formattedParagraphs = append(formattedParagraphs, "　　"+trimmed)
	}

	// 段与段之间自然空行
	return strings.Join(formattedParagraphs, "\n\n")
}

func applyCustomReplaceRegex(content string, replaceRegex string) string {
	parts := strings.Split(replaceRegex, "##")
	cur := content
	for i := 1; i < len(parts); i += 2 {
		pattern := parts[i]
		replacement := ""
		if i+1 < len(parts) {
			replacement = parts[i+1]
		}
		if pattern == "" {
			continue
		}

		// 支持 (?m) 跨行修饰
		re, err := regexp.Compile(pattern)
		if err == nil {
			cur = re.ReplaceAllString(cur, replacement)
		}
	}
	return cur
}
