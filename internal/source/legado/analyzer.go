package legado

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Analyzer struct {
	jsRuntime *JSRuntime
}

func NewAnalyzer(jsRuntime *JSRuntime) *Analyzer {
	return &Analyzer{jsRuntime: jsRuntime}
}

// ExtractString 根据 Legado 规则从 HTML 中提取单个字符串
func (a *Analyzer) ExtractString(doc *goquery.Document, rawRule string, baseURL string) (string, error) {
	rule := strings.TrimSpace(rawRule)
	if rule == "" {
		return "", nil
	}

	// 1. 优先检查内嵌 JS: <js>...</js> 或 @js:...
	if jsCode, ok := extractJS(rule); ok {
		html, _ := doc.Html()
		val, err := a.jsRuntime.Execute(jsCode, html, baseURL)
		if err != nil {
			return "", err
		}
		if str, ok := val.(string); ok {
			return str, nil
		}
		return fmt.Sprintf("%v", val), nil
	}

	// 2. 切分 || 或条件
	orRules := splitByPipePipe(rule)
	for _, subRule := range orRules {
		res := a.evalSingleRuleString(doc, subRule, baseURL)
		if strings.TrimSpace(res) != "" {
			return res, nil
		}
	}

	return "", nil
}

// ExtractList 根据 Legado 规则从 HTML 中提取元素或链接列表
func (a *Analyzer) ExtractList(doc *goquery.Document, rawRule string, baseURL string) ([]string, error) {
	rule := strings.TrimSpace(rawRule)
	if rule == "" {
		return nil, nil
	}

	// 1. 检查内嵌 JS
	if jsCode, ok := extractJS(rule); ok {
		html, _ := doc.Html()
		val, err := a.jsRuntime.Execute(jsCode, html, baseURL)
		if err != nil {
			return nil, err
		}
		if arr, ok := val.([]interface{}); ok {
			var list []string
			for _, item := range arr {
				list = append(list, fmt.Sprintf("%v", item))
			}
			return list, nil
		}
		if str, ok := val.(string); ok && str != "" {
			// 如果返回的是 HTML 字符串片段，将其按节点解析
			subDoc, err := goquery.NewDocumentFromReader(strings.NewReader(str))
			if err == nil {
				var list []string
				subDoc.Find("a, li, tr, p").Each(func(i int, s *goquery.Selection) {
					h, _ := goquery.OuterHtml(s)
					list = append(list, h)
				})
				if len(list) > 0 {
					return list, nil
				}
			}
			return []string{str}, nil
		}
	}

	// 2. CSS 规则解析
	selector, attr, regexReplace := parseRuleParts(rule)
	var list []string

	if selector == "" {
		selector = "body"
	}

	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		val := extractValueFromSelection(s, attr, baseURL)
		if regexReplace != nil {
			val = applyRegexReplace(val, regexReplace)
		}
		val = strings.TrimSpace(val)
		if val != "" {
			list = append(list, val)
		}
	})

	return list, nil
}

// evalSingleRuleString 处理单一选择器规则并应用正则后处理
func (a *Analyzer) evalSingleRuleString(doc *goquery.Document, rule string, baseURL string) string {
	selector, attr, regexReplace := parseRuleParts(rule)

	var val string
	if selector == "" || selector == "text" || selector == "html" {
		val = extractValueFromSelection(doc.Selection, attr, baseURL)
	} else {
		target := doc.Find(selector).First()
		if target.Length() > 0 {
			val = extractValueFromSelection(target, attr, baseURL)
		}
	}

	if regexReplace != nil {
		val = applyRegexReplace(val, regexReplace)
	}

	return strings.TrimSpace(val)
}

// parseRuleParts 将形如 "class.content@p@text##regex##replace" 切解为选择器、属性与正则链
func parseRuleParts(rule string) (selector string, attr string, regexChain [][]string) {
	clean := rule

	// 提取 ## 正则替换部分
	if idx := strings.Index(clean, "##"); idx >= 0 {
		regexPart := clean[idx:]
		clean = clean[:idx]

		parts := strings.Split(regexPart, "##")
		for i := 1; i < len(parts); i += 2 {
			if i+1 < len(parts) {
				regexChain = append(regexChain, []string{parts[i], parts[i+1]})
			} else {
				regexChain = append(regexChain, []string{parts[i], ""})
			}
		}
	}

	// 提取 @ 属性后缀
	if idx := strings.LastIndex(clean, "@"); idx >= 0 {
		attr = strings.TrimSpace(clean[idx+1:])
		clean = strings.TrimSpace(clean[:idx])
	} else {
		attr = "text"
	}

	// 标准化 Legado 的 class.xxx / id.xxx / tag.xxx 为 CSS 选择器
	selector = normalizeLegadoCSS(clean)
	return
}

func normalizeLegadoCSS(sel string) string {
	parts := strings.Split(sel, "@")
	var normParts []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		// 转换 class.xxx -> .xxx
		if strings.HasPrefix(trimmed, "class.") {
			cls := strings.TrimPrefix(trimmed, "class.")
			cls = strings.ReplaceAll(cls, " ", ".")
			normParts = append(normParts, "."+cls)
		} else if strings.HasPrefix(trimmed, "id.") {
			normParts = append(normParts, "#"+strings.TrimPrefix(trimmed, "id."))
		} else if strings.HasPrefix(trimmed, "tag.") {
			normParts = append(normParts, strings.TrimPrefix(trimmed, "tag."))
		} else {
			normParts = append(normParts, trimmed)
		}
	}
	return strings.Join(normParts, " ")
}

func extractValueFromSelection(s *goquery.Selection, attr string, baseURL string) string {
	if s == nil || s.Length() == 0 {
		return ""
	}

	switch strings.ToLower(attr) {
	case "text":
		return s.Text()
	case "html", "all":
		h, _ := s.Html()
		return h
	case "href":
		href, _ := s.Attr("href")
		if href != "" && baseURL != "" && !strings.HasPrefix(href, "http") {
			if base, err := url.Parse(baseURL); err == nil {
				if rel, err := base.Parse(href); err == nil {
					return rel.String()
				}
			}
		}
		return href
	case "src":
		src, _ := s.Attr("src")
		if src != "" && baseURL != "" && !strings.HasPrefix(src, "http") {
			if base, err := url.Parse(baseURL); err == nil {
				if rel, err := base.Parse(src); err == nil {
					return rel.String()
				}
			}
		}
		return src
	default:
		val, ok := s.Attr(attr)
		if ok {
			return val
		}
		return s.Text()
	}
}

func applyRegexReplace(input string, chain [][]string) string {
	cur := input
	for _, pair := range chain {
		pattern := pair[0]
		replaceWith := pair[1]
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err == nil {
			cur = re.ReplaceAllString(cur, replaceWith)
		}
	}
	return cur
}

func extractJS(rule string) (string, bool) {
	trimmed := strings.TrimSpace(rule)
	if strings.HasPrefix(trimmed, "<js>") && strings.HasSuffix(trimmed, "</js>") {
		code := strings.TrimPrefix(trimmed, "<js>")
		code = strings.TrimSuffix(code, "</js>")
		return strings.TrimSpace(code), true
	}
	if strings.HasPrefix(trimmed, "@js:") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "@js:")), true
	}
	if strings.HasPrefix(trimmed, "+@js:") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "+@js:")), true
	}
	return "", false
}

func splitByPipePipe(rule string) []string {
	parts := strings.Split(rule, "||")
	var res []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			res = append(res, t)
		}
	}
	return res
}
