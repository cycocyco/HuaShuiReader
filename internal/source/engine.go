package source

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"huashuireader/internal/domain"
	"huashuireader/internal/source/legado"
)

type RuleSearch struct {
	BookList    string `json:"bookList"`
	Name        string `json:"name"`
	Author      string `json:"author"`
	BookURL     string `json:"bookUrl"`
	LastChapter string `json:"lastChapter"`
	Intro       string `json:"intro"`
}

type RuleBookInfo struct {
	Init        string `json:"init"`
	Name        string `json:"name"`
	Author      string `json:"author"`
	Intro       string `json:"intro"`
	TocURL      string `json:"tocUrl"`
	LastChapter string `json:"lastChapter"`
}

type RuleToc struct {
	ChapterList string `json:"chapterList"`
	ChapterName string `json:"chapterName"`
	ChapterURL  string `json:"chapterUrl"`
	NextTocURL  string `json:"nextTocUrl"`
}

type RuleContent struct {
	Content      string `json:"content"`
	NextURL      string `json:"nextContentUrl"`
	WebJS        string `json:"webJs"`
	ReplaceRegex string `json:"replaceRegex"`
	Title        string `json:"title"`
}

// LegadoBookSource 书源模型
type LegadoBookSource struct {
	BookSourceName string        `json:"bookSourceName"`
	BookSourceURL  string        `json:"bookSourceUrl"`
	SearchURL      string        `json:"searchUrl"`
	RuleSearch     *RuleSearch   `json:"ruleSearch"`
	RuleBookInfo   *RuleBookInfo `json:"ruleBookInfo"`
	RuleToc        *RuleToc      `json:"ruleToc"`
	RuleContent    *RuleContent  `json:"ruleContent"`
	Enabled        bool          `json:"enabled"`
}

// SearchResultItem 聚合搜索结果
type SearchResultItem struct {
	SourceID    string `json:"sourceId"`
	SourceName  string `json:"sourceName"`
	SourceURL   string `json:"sourceUrl"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	BookURL     string `json:"bookUrl"`
	LastChapter string `json:"lastChapter"`
	Intro       string `json:"intro"`
}

type Engine struct {
	client    *http.Client
	jsRuntime *legado.JSRuntime
	analyzer  *legado.Analyzer
}

func NewEngine() *Engine {
	// 配置标准安全 Transport，支持 TLS 1.2/1.3，支持大文件流式接收与 Keep-Alive
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   60 * time.Second, // 增大超时时间以支持 10MB+ 超大型聚合书源包下载
	}

	jsRuntime := legado.NewJSRuntime(client)
	analyzer := legado.NewAnalyzer(jsRuntime)

	return &Engine{
		client:    client,
		jsRuntime: jsRuntime,
		analyzer:  analyzer,
	}
}

// ParseSourcesFromFile 从本地文件读取解析书源
func (e *Engine) ParseSourcesFromFile(filePath string) ([]LegadoBookSource, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取本地文件失败: %w", err)
	}
	return e.ParseSourcesFromJSON(string(data))
}

// ParseSourcesFromJSON 从 JSON 字符串解析书源列表，支持容错与带注释的 JSON
func (e *Engine) ParseSourcesFromJSON(rawJSON string) ([]LegadoBookSource, error) {
	trimmed := strings.TrimSpace(rawJSON)

	// 容错 1：如果用户粘贴的是网络链接
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return e.FetchSourcesFromURL(trimmed)
	}

	// 容错 2：如果用户粘贴的是本地文件路径 (如 W:\... 或 C:\... 或以 .json 结尾的文件)
	if fi, err := os.Stat(trimmed); err == nil && !fi.IsDir() {
		return e.ParseSourcesFromFile(trimmed)
	}

	var list []LegadoBookSource

	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
			return nil, fmt.Errorf("解析书源数组失败: %v", err)
		}
	} else if strings.HasPrefix(trimmed, "{") {
		var single LegadoBookSource
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return nil, fmt.Errorf("解析单个书源失败: %v", err)
		}
		list = append(list, single)
	} else {
		return nil, fmt.Errorf("无效的书源格式: 请输入有效的 JSON 内容、网络链接或本地文件路径")
	}

	for i := range list {
		list[i].Enabled = true
	}

	return list, nil
}

// FetchSourcesFromURL 从网络 URL 订阅书源
func (e *Engine) FetchSourcesFromURL(targetURL string) ([]LegadoBookSource, error) {
	// 如果传入的是本地绝对/相对文件路径，直接本地读取
	if fi, err := os.Stat(targetURL); err == nil && !fi.IsDir() {
		return e.ParseSourcesFromFile(targetURL)
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求错误: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载书源失败，服务器返回: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取书源数据流失败: %w", err)
	}

	return e.ParseSourcesFromJSON(string(body))
}

// SearchBooksAcrossSources 并发多源跨站聚合搜索（24 并发，全局 25s，单源 5s 独立超时）
func (e *Engine) SearchBooksAcrossSources(sources []LegadoBookSource, keyword string) []SearchResultItem {
	var results []SearchResultItem
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 并发上限保护
	semaphore := make(chan struct{}, 24)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	for _, s := range sources {
		if !s.Enabled || s.SearchURL == "" {
			continue
		}

		src := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}

			// 单源 5s 独立超时保护
			singleCtx, singleCancel := context.WithTimeout(ctx, 5*time.Second)
			defer singleCancel()

			items, err := e.searchSingleSource(singleCtx, src, keyword)
			if err == nil && len(items) > 0 {
				mu.Lock()
				results = append(results, items...)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return results
}

func (e *Engine) searchSingleSource(ctx context.Context, src LegadoBookSource, keyword string) ([]SearchResultItem, error) {
	targetURL := strings.ReplaceAll(src.SearchURL, "{{key}}", url.QueryEscape(keyword))
	targetURL = strings.ReplaceAll(targetURL, "{key}", url.QueryEscape(keyword))

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		baseURL := src.BookSourceURL
		if !strings.HasSuffix(baseURL, "/") && !strings.HasPrefix(targetURL, "/") {
			baseURL += "/"
		}
		targetURL = baseURL + targetURL
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var items []SearchResultItem

	// 规则解析器：优先 CSS 选择器，兜底通用解析
	bookListRule := "ul li, .item, .book-item, .search-item, tbody tr"
	if src.RuleSearch != nil && src.RuleSearch.BookList != "" {
		bookListRule = src.RuleSearch.BookList
	}

	doc.Find(bookListRule).Each(func(i int, s *goquery.Selection) {
		if i >= 10 { // 单源最多摘取前 10 条
			return
		}

		title := ""
		bookURL := ""
		author := ""
		lastChap := ""

		// 解析链接与书名
		aTag := s.Find("a").First()
		if aTag.Length() > 0 {
			title = strings.TrimSpace(aTag.Text())
			bookURL, _ = aTag.Attr("href")
		}

		if src.RuleSearch != nil {
			if src.RuleSearch.Name != "" {
				t := strings.TrimSpace(s.Find(src.RuleSearch.Name).Text())
				if t != "" {
					title = t
				}
			}
			if src.RuleSearch.BookURL != "" {
				u, _ := s.Find(src.RuleSearch.BookURL).Attr("href")
				if u != "" {
					bookURL = u
				}
			}
			if src.RuleSearch.Author != "" {
				author = strings.TrimSpace(s.Find(src.RuleSearch.Author).Text())
			}
			if src.RuleSearch.LastChapter != "" {
				lastChap = strings.TrimSpace(s.Find(src.RuleSearch.LastChapter).Text())
			}
		}

		// 补全相对 URL
		if bookURL != "" && !strings.HasPrefix(bookURL, "http") {
			parsedBase, _ := url.Parse(src.BookSourceURL)
			if parsedBase != nil {
				parsedRel, err := parsedBase.Parse(bookURL)
				if err == nil {
					bookURL = parsedRel.String()
				}
			}
		}

		// 只要匹配书名包含关键字或有效书名即可
		if title != "" && bookURL != "" && (strings.Contains(title, keyword) || keyword == "") {
			items = append(items, SearchResultItem{
				SourceID:    src.BookSourceURL,
				SourceName:  src.BookSourceName,
				SourceURL:   src.BookSourceURL,
				Title:       title,
				Author:      author,
				BookURL:     bookURL,
				LastChapter: lastChap,
			})
		}
	})

	return items, nil
}

func (e *Engine) readHTMLAsUTF8(resp *http.Response) (string, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 探测是否为 GBK / GB18030 / GB2312
	detector := chardet.NewTextDetector()
	res, err := detector.DetectBest(raw)
	needGBK := false
	if err == nil && res != nil {
		enc := strings.ToLower(res.Charset)
		if strings.Contains(enc, "gb") || enc == "gb18030" || enc == "gbk" || enc == "gb2312" {
			needGBK = true
		}
	}
	headLimit := len(raw)
	if headLimit > 1024 {
		headLimit = 1024
	}
	if bytes.Contains(bytes.ToLower(raw[:headLimit]), []byte("charset=gb")) {
		needGBK = true
	}

	if needGBK {
		reader := transform.NewReader(bytes.NewReader(raw), simplifiedchinese.GB18030.NewDecoder())
		utf8Data, err := io.ReadAll(reader)
		if err == nil {
			return string(utf8Data), nil
		}
	}

	return string(raw), nil
}

// FetchChaptersFromWeb 从在线网页中智能解析小说章节目录
func (e *Engine) FetchChaptersFromWeb(bookURL string, bookID string) ([]domain.Chapter, error) {
	req, err := http.NewRequest("GET", bookURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求小说目录失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("目录响应异常: HTTP %d", resp.StatusCode)
	}

	htmlStr, err := e.readHTMLAsUTF8(resp)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return nil, err
	}

	baseURL, _ := url.Parse(bookURL)
	var chapters []domain.Chapter

	// 优先级选择器候选
	selectors := []string{
		"#list a", "#chapterlist a", ".chapter-list a", ".catalog a", ".dir a", "dl dd a",
		".section-box a", "ul.chapters a", ".book-list a", "#chapters a",
	}

	for _, sel := range selectors {
		found := doc.Find(sel)
		if found.Length() > 0 {
			found.Each(func(i int, s *goquery.Selection) {
				t := strings.TrimSpace(s.Text())
				href, ok := s.Attr("href")
				if ok && href != "" && !strings.HasPrefix(href, "javascript") && href != "#" {
					if baseURL != nil {
						if rel, err := baseURL.Parse(href); err == nil {
							href = rel.String()
						}
					}
					if t != "" {
						chapters = append(chapters, domain.Chapter{
							ID:       fmt.Sprintf("%s_%d", bookID, len(chapters)),
							BookID:   bookID,
							Index:    len(chapters),
							Title:    t,
							URL:      href,
							IsCached: false,
						})
					}
				}
			})
			if len(chapters) > 0 {
				break
			}
		}
	}

	// 若特定容器未找到，兜底全文 <a> 链接检索包含章节特征的元素
	if len(chapters) == 0 {
		chapPattern := regexp.MustCompile(`(第[0-9一二三四五六七八九十百千万]+[章回节卷]|序章|尾声|番外|Chapter)`)
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			t := strings.TrimSpace(s.Text())
			if chapPattern.MatchString(t) && len(t) <= 40 {
				href, ok := s.Attr("href")
				if ok && href != "" && !strings.HasPrefix(href, "javascript") && href != "#" {
					if baseURL != nil {
						if rel, err := baseURL.Parse(href); err == nil {
							href = rel.String()
						}
					}
					chapters = append(chapters, domain.Chapter{
						ID:       fmt.Sprintf("%s_%d", bookID, len(chapters)),
						BookID:   bookID,
						Index:    len(chapters),
						Title:    t,
						URL:      href,
						IsCached: false,
					})
				}
			}
		})
	}

	if len(chapters) == 0 {
		return nil, fmt.Errorf("未能从该网页提取到有效目录，请换一个源试看")
	}

	return chapters, nil
}

// FetchChapterContentFromWeb 从在线章节页面抓取并净化正文
func (e *Engine) FetchChapterContentFromWeb(chapterURL string) (string, error) {
	req, err := http.NewRequest("GET", chapterURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求章节内容失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("章节响应错误: HTTP %d", resp.StatusCode)
	}

	htmlStr, err := e.readHTMLAsUTF8(resp)
	if err != nil {
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return "", err
	}

	// 剔除无用标签与干扰脚本
	doc.Find("script, style, iframe, header, footer, nav, .header, .footer, .ad, .advertisement, [id*='ad'], [class*='ad'], a").Remove()

	contentSelectors := []string{
		"#content", "#chaptercontent", "#BookText", ".content", ".read-content",
		"#htmlContent", ".novel-content", "#novelcontent", "#chapter-content", "#txtContent",
		"#nr1", ".txt",
	}

	var rawHTML string
	for _, sel := range contentSelectors {
		selTarget := doc.Find(sel)
		if selTarget.Length() > 0 {
			h, err := selTarget.Html()
			if err == nil && len(strings.TrimSpace(h)) > 50 {
				rawHTML = h
				break
			}
		}
	}

	// 兜底找字数最多的 div
	if rawHTML == "" {
		maxLen := 0
		doc.Find("div").Each(func(i int, s *goquery.Selection) {
			t := strings.TrimSpace(s.Text())
			if len(t) > maxLen {
				maxLen = len(t)
				rawHTML, _ = s.Html()
			}
		})
	}

	if rawHTML == "" {
		return "", fmt.Errorf("未能解析出章节正文内容")
	}

	// HTML 净化与段落还原
	cleanText := rawHTML
	cleanText = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(cleanText, "\n")
	cleanText = regexp.MustCompile(`(?i)</p>`).ReplaceAllString(cleanText, "\n")
	cleanText = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(cleanText, "")
	cleanText = html.UnescapeString(cleanText)

	// 过滤垃圾广告
	lines := strings.Split(cleanText, "\n")
	var resultLines []string
	adPattern := regexp.MustCompile(`(未完待续|最新网址|收藏本站|请记住本站|网页版章节|笔趣阁|加入书签|推荐票|上一页|下一页)`)
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if adPattern.MatchString(l) && len(l) < 30 {
			continue
		}
		resultLines = append(resultLines, "　　"+l)
	}

	return strings.Join(resultLines, "\n\n"), nil
}

// FetchChaptersWithSource 根据 Legado 书源规则提取章节目录，若失败自动降级到启发式
func (e *Engine) FetchChaptersWithSource(bookURL string, bookID string, src *LegadoBookSource) ([]domain.Chapter, error) {
	if src == nil || src.RuleToc == nil || src.RuleToc.ChapterList == "" {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}

	tocURL := bookURL
	// 如果详情页规则配置了专门的目录页 tocUrl，先解析出真实目录页链接
	if src.RuleBookInfo != nil && src.RuleBookInfo.TocURL != "" {
		req, err := http.NewRequest("GET", bookURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			resp, err := e.client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				htmlStr, _ := e.readHTMLAsUTF8(resp)
				resp.Body.Close()
				doc, _ := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
				extractedToc, _ := e.analyzer.ExtractString(doc, src.RuleBookInfo.TocURL, bookURL)
				if extractedToc != "" && strings.HasPrefix(extractedToc, "http") {
					tocURL = extractedToc
				}
			}
		}
	}

	req, err := http.NewRequest("GET", tocURL, nil)
	if err != nil {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := e.client.Do(req)
	if err != nil {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}

	htmlStr, err := e.readHTMLAsUTF8(resp)
	if err != nil {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}

	rawItems, err := e.analyzer.ExtractList(doc, src.RuleToc.ChapterList, tocURL)
	if err != nil || len(rawItems) == 0 {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}

	var chapters []domain.Chapter
	baseURL, _ := url.Parse(tocURL)

	for _, itemHTML := range rawItems {
		subDoc, err := goquery.NewDocumentFromReader(strings.NewReader(itemHTML))
		if err != nil {
			continue
		}

		title := ""
		chapURL := ""

		if src.RuleToc.ChapterName != "" {
			title, _ = e.analyzer.ExtractString(subDoc, src.RuleToc.ChapterName, tocURL)
		}
		if title == "" {
			title = strings.TrimSpace(subDoc.Find("a").Text())
			if title == "" {
				title = strings.TrimSpace(subDoc.Text())
			}
		}

		if src.RuleToc.ChapterURL != "" {
			chapURL, _ = e.analyzer.ExtractString(subDoc, src.RuleToc.ChapterURL, tocURL)
		}
		if chapURL == "" {
			chapURL, _ = subDoc.Find("a").Attr("href")
		}

		if chapURL != "" && baseURL != nil && !strings.HasPrefix(chapURL, "http") {
			if rel, err := baseURL.Parse(chapURL); err == nil {
				chapURL = rel.String()
			}
		}

		if title != "" && chapURL != "" {
			chapters = append(chapters, domain.Chapter{
				ID:       fmt.Sprintf("%s_%d", bookID, len(chapters)),
				BookID:   bookID,
				Index:    len(chapters),
				Title:    title,
				URL:      chapURL,
				IsCached: false,
			})
		}
	}

	if len(chapters) == 0 {
		return e.FetchChaptersFromWeb(bookURL, bookID)
	}

	return chapters, nil
}

// FetchContentWithSource 根据 Legado 书源规则提取并净化正文，支持正文多页自动合并
func (e *Engine) FetchContentWithSource(chapterURL string, chapterTitle string, src *LegadoBookSource) (string, error) {
	if src == nil || src.RuleContent == nil || src.RuleContent.Content == "" {
		return e.FetchChapterContentFromWeb(chapterURL)
	}

	fullRawHTML := ""
	curURL := chapterURL
	visitedURLs := make(map[string]bool)

	// 分页循环抓取拼接（最多支持单章 15 页防死循环）
	for page := 0; page < 15; page++ {
		if curURL == "" || visitedURLs[curURL] {
			break
		}
		visitedURLs[curURL] = true

		req, err := http.NewRequest("GET", curURL, nil)
		if err != nil {
			break
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := e.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			break
		}

		htmlStr, err := e.readHTMLAsUTF8(resp)
		resp.Body.Close()
		if err != nil {
			break
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
		if err != nil {
			break
		}

		// 使用 Legado 规则提取该页正文
		pageContent, err := e.analyzer.ExtractString(doc, src.RuleContent.Content, curURL)
		if err != nil || pageContent == "" {
			pageContent = e.heuristicExtractContent(doc)
		}

		if pageContent != "" {
			if fullRawHTML != "" {
				fullRawHTML += "\n\n"
			}
			fullRawHTML += pageContent
		}

		// 检查下一页链接规则 (nextContentUrl)
		if src.RuleContent.NextURL == "" {
			break
		}

		nextURL, err := e.analyzer.ExtractString(doc, src.RuleContent.NextURL, curURL)
		if err != nil || nextURL == "" || nextURL == curURL {
			break
		}
		if !strings.HasPrefix(nextURL, "http") {
			if base, err := url.Parse(curURL); err == nil {
				if rel, err := base.Parse(nextURL); err == nil {
					nextURL = rel.String()
				}
			}
		}

		curURL = nextURL
	}

	if strings.TrimSpace(fullRawHTML) == "" {
		return e.FetchChapterContentFromWeb(chapterURL)
	}

	replaceRegex := ""
	if src.RuleContent != nil {
		replaceRegex = src.RuleContent.ReplaceRegex
	}

	// 依照阅读规范深度净化与段落排版
	return legado.CleanAndFormatContent(fullRawHTML, chapterTitle, replaceRegex), nil
}

func (e *Engine) heuristicExtractContent(doc *goquery.Document) string {
	doc.Find("script, style, iframe, header, footer, nav, [class*='ad']").Remove()
	contentSelectors := []string{
		"#content", "#chaptercontent", "#BookText", ".content", ".read-content",
		"#htmlContent", ".novel-content", "#novelcontent", "#chapter-content", "#txtContent",
	}
	for _, sel := range contentSelectors {
		target := doc.Find(sel)
		if target.Length() > 0 {
			h, err := target.Html()
			if err == nil && len(strings.TrimSpace(h)) > 30 {
				return h
			}
		}
	}
	return ""
}
