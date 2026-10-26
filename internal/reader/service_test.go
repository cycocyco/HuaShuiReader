package reader

import (
	"huashuireader/internal/cache"
	"huashuireader/internal/library"
	"huashuireader/internal/local"
	"huashuireader/internal/source"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderService_LocalBookFlow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "reader_test_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := library.NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	cacheMgr, err := cache.NewManager(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	parser := local.NewParser()
	engine := source.NewEngine()

	svc := NewService(store, parser, cacheMgr, engine)

	// 创建测试小说文件
	txtFile := filepath.Join(tempDir, "test.txt")
	content := "序章 序曲\n这里是前言内容。\n\n第1章 崭新的世界\n这是第一章的正文内容。\n\n第2章 启程\n这是第二章的正文内容。"
	if err := os.WriteFile(txtFile, []byte(content), 0644); err != nil {
		t.Fatalf("write txt failed: %v", err)
	}

	book, err := parser.ImportBook(txtFile, filepath.Join(tempDir, "books"))
	if err != nil {
		t.Fatalf("ImportBook failed: %v", err)
	}
	if err := store.SaveBook(book); err != nil {
		t.Fatalf("SaveBook failed: %v", err)
	}

	// 1. 获取目录
	chaps, err := svc.GetChapterList(book.ID)
	if err != nil || len(chaps) < 3 {
		t.Fatalf("GetChapterList failed, got: %d chaps, err: %v", len(chaps), err)
	}

	// 2. 读取第一章
	c1, err := svc.ReadChapter(book.ID, 1)
	if err != nil || c1 == nil {
		t.Fatalf("ReadChapter 1 failed: %v", err)
	}
	if c1.Title != "第1章 崭新的世界" || len(c1.Content) == 0 {
		t.Fatalf("ReadChapter content mismatch: %+v", c1)
	}
	if !c1.HasNext || !c1.HasPrev {
		t.Fatalf("HasNext/HasPrev mismatch: %+v", c1)
	}
}
