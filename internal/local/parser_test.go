package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func TestParser_DetectAndScanChapters(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "parser_test_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	parser := NewParser()

	content := "前言说明\n\n第1章 崭新的世界\n这是第一章的正文内容。\n\n第2章 风起云涌\n这是第二章的内容。"

	// 1. 测试 UTF-8
	utf8File := filepath.Join(tempDir, "utf8.txt")
	if err := os.WriteFile(utf8File, []byte(content), 0644); err != nil {
		t.Fatalf("write utf8 file failed: %v", err)
	}
	enc, err := parser.DetectEncoding(utf8File)
	if err != nil || enc != "utf-8" {
		t.Fatalf("DetectEncoding UTF-8 failed, got: %s", enc)
	}
	chaps, err := parser.ScanChapters(utf8File, enc)
	if err != nil || len(chaps) < 3 {
		t.Fatalf("ScanChapters failed, expected at least 3 parts, got %d", len(chaps))
	}

	// 2. 测试 GB18030 转码与读取
	gbkBytes, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(content))
	gbkFile := filepath.Join(tempDir, "gbk.txt")
	if err := os.WriteFile(gbkFile, gbkBytes, 0644); err != nil {
		t.Fatalf("write gbk file failed: %v", err)
	}
	encGBK, _ := parser.DetectEncoding(gbkFile)
	chapsGBK, err := parser.ScanChapters(gbkFile, encGBK)
	if err != nil || len(chapsGBK) < 3 {
		t.Fatalf("ScanChapters GBK failed, got %d chapters", len(chapsGBK))
	}

	// 3. 测试 Big5
	big5Text := "第1章 繁體中文測試\n這是繁體內容。\n第2章 繼續推進\n這是第二段。"
	big5Bytes, _ := traditionalchinese.Big5.NewEncoder().Bytes([]byte(big5Text))
	big5File := filepath.Join(tempDir, "big5.txt")
	if err := os.WriteFile(big5File, big5Bytes, 0644); err != nil {
		t.Fatalf("write big5 file failed: %v", err)
	}
	r := parser.ConvertToUTF8Reader(strings.NewReader(string(big5Bytes)), "big5")
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "繁體中文測試") {
		t.Fatalf("Big5 decoding failed: %s", string(buf[:n]))
	}

	// 4. 导入书籍
	booksDir := filepath.Join(tempDir, "books")
	book, err := parser.ImportBook(utf8File, booksDir)
	if err != nil || book.TotalChaps == 0 {
		t.Fatalf("ImportBook failed: %v, book: %+v", err, book)
	}
}
