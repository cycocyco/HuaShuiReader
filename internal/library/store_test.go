package library

import (
	"huashuireader/internal/domain"
	"huashuireader/internal/source"
	"os"
	"testing"
	"time"
)

func TestStore_BookAndProgressLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "store_test_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer s.Close()

	// 1. 保存书籍
	book1 := domain.Book{
		ID:         "book_001",
		Title:      "测试小说1",
		Author:     "作者甲",
		SourceType: "local_txt",
		BookURL:    "test.txt",
		TotalChaps: 10,
	}
	if err := s.SaveBook(book1); err != nil {
		t.Fatalf("SaveBook failed: %v", err)
	}

	book2 := domain.Book{
		ID:         "book_002",
		Title:      "测试小说2",
		Author:     "作者乙",
		SourceType: "online",
		BookURL:    "http://example.com/2",
		TotalChaps: 20,
	}
	if err := s.SaveBook(book2); err != nil {
		t.Fatalf("SaveBook 2 failed: %v", err)
	}

	// 2. 检查书籍列表
	books, err := s.ListBooks()
	if err != nil || len(books) != 2 {
		t.Fatalf("ListBooks count mismatch: got %d, expected 2", len(books))
	}

	// 3. 记录阅读进度
	prog1 := domain.Progress{
		BookID:       "book_001",
		ChapterIndex: 2,
		ChapterTitle: "第二章",
		CharOffset:   120,
		Ratio:        0.2,
		UpdatedAt:    time.Now().Add(-1 * time.Hour),
	}
	if err := s.SaveProgress(prog1); err != nil {
		t.Fatalf("SaveProgress 1 failed: %v", err)
	}

	prog2 := domain.Progress{
		BookID:       "book_002",
		ChapterIndex: 5,
		ChapterTitle: "第五章",
		CharOffset:   500,
		Ratio:        0.5,
		UpdatedAt:    time.Now(),
	}
	if err := s.SaveProgress(prog2); err != nil {
		t.Fatalf("SaveProgress 2 failed: %v", err)
	}

	// 验证最近阅读应该为 book_002
	lastID, err := s.GetLastReadBookID()
	if err != nil || lastID != "book_002" {
		t.Fatalf("Expected last read book_002, got %s, err: %v", lastID, err)
	}

	// 4. 删除 book_002，验证级联和孤儿防范
	deletedBook, err := s.DeleteBook("book_002")
	if err != nil || deletedBook == nil || deletedBook.ID != "book_002" {
		t.Fatalf("DeleteBook failed: %v", err)
	}

	// 验证 GetLastReadBookID 不应返回已被删除的 book_002，而应退回 book_001
	lastIDAfterDel, err := s.GetLastReadBookID()
	if err != nil || lastIDAfterDel != "book_001" {
		t.Fatalf("Expected last read fallback to book_001, got %s", lastIDAfterDel)
	}

	// 验证 progress 查询已被级联清理
	pAfter, err := s.GetProgress("book_002")
	if err != nil || pAfter != nil {
		t.Fatalf("Progress for book_002 should be nil after delete, got %+v", pAfter)
	}
}

func TestStore_SaveSources(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sources_test_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer s.Close()

	srcList := []source.LegadoBookSource{
		{BookSourceName: "有效源1", BookSourceURL: "https://src1.com", Enabled: true},
		{BookSourceName: "", BookSourceURL: "https://invalid.com", Enabled: true}, // 无效源，应当跳过
		{BookSourceName: "有效源2", BookSourceURL: "https://src2.com", Enabled: false},
	}

	n, err := s.SaveSources(srcList)
	if err != nil {
		t.Fatalf("SaveSources failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("Expected 2 valid sources saved, got %d", n)
	}

	all, err := s.GetAllSources()
	if err != nil || len(all) != 2 {
		t.Fatalf("GetAllSources count mismatch: got %d", len(all))
	}
}
