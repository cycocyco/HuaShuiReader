package local

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
	"huashuireader/internal/domain"
)

var chapterRegex = regexp.MustCompile(`(?m)^\s*(第[0-9一二三四五六七八九十百千万]+[章回节卷集幕部篇].{0,40}|Chapter\s+[0-9]+.*|序章|尾声|番外.*)$`)

type Parser struct {
	detector *chardet.Detector
}

func NewParser() *Parser {
	return &Parser{
		detector: chardet.NewTextDetector(),
	}
}

func (p *Parser) GenerateBookID(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	raw := fmt.Sprintf("%s|%d|%d", filepath.Clean(path), fi.Size(), fi.ModTime().Unix())
	h := md5.Sum([]byte(raw))
	return hex.EncodeToString(h[:]), nil
}

// DetectEncoding 精准检测编码：UTF-8, GB18030, Big5, UTF-16LE, UTF-16BE
func (p *Parser) DetectEncoding(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "utf-8", err
	}
	defer file.Close()

	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return "utf-8", err
	}
	buf = buf[:n]

	// 1. 检查 UTF-8 BOM
	if bytes.HasPrefix(buf, []byte{0xEF, 0xBB, 0xBF}) {
		return "utf-8", nil
	}

	// 2. 检查 UTF-16 BOM
	if bytes.HasPrefix(buf, []byte{0xFF, 0xFE}) {
		return "utf-16le", nil
	}
	if bytes.HasPrefix(buf, []byte{0xFE, 0xFF}) {
		return "utf-16be", nil
	}

	// 3. 使用专业文本编码探测器
	res, err := p.detector.DetectBest(buf)
	if err == nil && res != nil {
		enc := strings.ToLower(res.Charset)
		if strings.Contains(enc, "big5") {
			return "big5", nil
		}
		if strings.Contains(enc, "gb") || enc == "gb18030" || enc == "gbk" || enc == "gb2312" {
			return "gb18030", nil
		}
		if enc == "utf-8" && res.Confidence > 60 {
			return "utf-8", nil
		}
	}

	// 4. 纯 UTF-8 校验
	if utf8.Valid(buf) {
		return "utf-8", nil
	}

	return "gb18030", nil
}

type ChapterOffset struct {
	Title     string
	RuneStart int
	RuneEnd   int
}

// ConvertToUTF8Reader 将原文件流统一转码为标准的 UTF-8 流，包含真正的 Big5 与 UTF-16 解码
func (p *Parser) ConvertToUTF8Reader(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(encoding) {
	case "gb18030", "gbk", "gb2312":
		return transform.NewReader(r, simplifiedchinese.GB18030.NewDecoder())
	case "big5":
		return transform.NewReader(r, traditionalchinese.Big5.NewDecoder())
	case "utf-16le":
		return transform.NewReader(r, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder())
	case "utf-16be":
		return transform.NewReader(r, unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder())
	default:
		return r
	}
}

// ScanChapters 流式扫描全书章节目录
func (p *Parser) ScanChapters(path string, encoding string) ([]ChapterOffset, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := p.ConvertToUTF8Reader(file, encoding)
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var list []ChapterOffset
	var currentTitle string = "序言 / 开始"
	var currentStart int = 0
	var runePos int = 0

	for scanner.Scan() {
		line := scanner.Text()
		lineRuneLen := len([]rune(line)) + 1 // +1 代表换行符

		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && len([]rune(trimmed)) <= 40 && chapterRegex.MatchString(trimmed) {
			if runePos > currentStart {
				list = append(list, ChapterOffset{
					Title:     currentTitle,
					RuneStart: currentStart,
					RuneEnd:   runePos,
				})
			}
			currentTitle = trimmed
			currentStart = runePos
		}
		runePos += lineRuneLen
	}

	if runePos > currentStart {
		list = append(list, ChapterOffset{
			Title:     currentTitle,
			RuneStart: currentStart,
			RuneEnd:   runePos,
		})
	}

	if len(list) == 0 {
		list = append(list, ChapterOffset{
			Title:     "全书内容",
			RuneStart: 0,
			RuneEnd:   runePos,
		})
	}

	return list, nil
}

// ReadChapterRange 按字符范围精准读取单章内容，自动剥离与标题重复的首行
func (p *Parser) ReadChapterRange(path string, encoding string, startRune, endRune int, chapterTitle string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	reader := p.ConvertToUTF8Reader(file, encoding)
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var sb strings.Builder
	var currentRunePos int = 0
	firstLine := true

	for scanner.Scan() {
		line := scanner.Text()
		lineRuneLen := len([]rune(line)) + 1

		if currentRunePos+lineRuneLen > startRune && currentRunePos < endRune {
			trimmed := strings.TrimSpace(line)
			// 若首行就是本章标题本身，剥离重复标题提高阅读呼吸感
			if firstLine && chapterTitle != "" && trimmed == strings.TrimSpace(chapterTitle) {
				firstLine = false
				currentRunePos += lineRuneLen
				continue
			}
			firstLine = false
			sb.WriteString(line)
			sb.WriteString("\n")
		}

		currentRunePos += lineRuneLen
		if currentRunePos >= endRune {
			break
		}
	}

	return sb.String(), nil
}

// ImportBook 完整解析并复制书籍到本地书库目录
func (p *Parser) ImportBook(srcPath string, libraryBooksDir string) (domain.Book, error) {
	if err := os.MkdirAll(libraryBooksDir, 0755); err != nil {
		return domain.Book{}, err
	}

	bookID, err := p.GenerateBookID(srcPath)
	if err != nil {
		return domain.Book{}, err
	}

	baseName := filepath.Base(srcPath)
	ext := filepath.Ext(baseName)
	cleanTitle := strings.TrimSuffix(baseName, ext)
	author := "未知作者"

	// 智能解析书名和作者，如 "书名 (作者)" 或 "[作者] 书名" 或 "书名-作者"
	if strings.Contains(cleanTitle, "-") {
		parts := strings.SplitN(cleanTitle, "-", 2)
		cleanTitle = strings.TrimSpace(parts[0])
		author = strings.TrimSpace(parts[1])
	} else if strings.Contains(cleanTitle, "_") {
		parts := strings.SplitN(cleanTitle, "_", 2)
		cleanTitle = strings.TrimSpace(parts[0])
		author = strings.TrimSpace(parts[1])
	}

	destPath := filepath.Join(libraryBooksDir, fmt.Sprintf("%s%s", bookID, ext))

	// 复制到本地书库目录，形成独立便携副本
	if filepath.Clean(srcPath) != filepath.Clean(destPath) {
		if err := copyFile(srcPath, destPath); err != nil {
			return domain.Book{}, fmt.Errorf("copy book file failed: %w", err)
		}
	}

	enc, _ := p.DetectEncoding(destPath)
	chaps, _ := p.ScanChapters(destPath, enc)

	lastChapTitle := "无章节"
	if len(chaps) > 0 {
		lastChapTitle = chaps[len(chaps)-1].Title
	}

	sourceType := "local_txt"
	if strings.ToLower(ext) == ".md" || strings.ToLower(ext) == ".markdown" {
		sourceType = "local_md"
	}

	return domain.Book{
		ID:          bookID,
		Title:       cleanTitle,
		Author:      author,
		SourceType:  sourceType,
		SourceID:    "",
		BookURL:     destPath,
		LastChapter: lastChapTitle,
		TotalChaps:  len(chaps),
	}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
