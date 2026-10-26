package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheManager_SaveGetSwitchClear(t *testing.T) {
	tempDir1, err := os.MkdirTemp("", "cache_test1_*")
	if err != nil {
		t.Fatalf("create temp dir 1 failed: %v", err)
	}
	defer os.RemoveAll(tempDir1)

	tempDir2, err := os.MkdirTemp("", "cache_test2_*")
	if err != nil {
		t.Fatalf("create temp dir 2 failed: %v", err)
	}
	defer os.RemoveAll(tempDir2)

	mgr, err := NewManager(tempDir1)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	bookID := "book_test_101"
	title := "第一章 初出茅庐"
	content := "这里是第一章的精彩正文内容。"

	// 1. 保存缓存
	if err := mgr.SaveChapter(bookID, 1, title, content); err != nil {
		t.Fatalf("SaveChapter failed: %v", err)
	}

	// 2. 读取缓存并验证标题校验
	got, ok := mgr.GetChapter(bookID, 1, title)
	if !ok || got != content {
		t.Fatalf("GetChapter failed or content mismatch: %s", got)
	}

	// 标题不匹配防串章测试
	_, okMismatch := mgr.GetChapter(bookID, 1, "完全不相关的第九十九章")
	if okMismatch {
		t.Fatalf("Expected title mismatch check to fail, but it succeeded")
	}

	// 3. 统计容量
	stats := mgr.GetCacheStats()
	if stats.TotalFiles != 1 || stats.TotalBytes == 0 {
		t.Fatalf("Stats mismatch: %+v", stats)
	}

	// 4. 切换目录并验证迁移
	if err := mgr.SwitchDir(tempDir2); err != nil {
		t.Fatalf("SwitchDir failed: %v", err)
	}
	gotAfterMigrate, ok2 := mgr.GetChapter(bookID, 1, title)
	if !ok2 || gotAfterMigrate != content {
		t.Fatalf("Cache not found after migration: %s", gotAfterMigrate)
	}

	// 5. 单书清理
	if err := mgr.ClearBookCache(bookID); err != nil {
		t.Fatalf("ClearBookCache failed: %v", err)
	}
	_, ok3 := mgr.GetChapter(bookID, 1, title)
	if ok3 {
		t.Fatalf("Expected chapter to be deleted after ClearBookCache")
	}

	// 6. 全局清理
	_ = mgr.SaveChapter("other_book", 1, "章名", "内容")
	if err := mgr.ClearAllCache(); err != nil {
		t.Fatalf("ClearAllCache failed: %v", err)
	}
	statsAfter := mgr.GetCacheStats()
	if statsAfter.TotalFiles != 0 {
		t.Fatalf("Expected 0 files after ClearAllCache, got %d", statsAfter.TotalFiles)
	}
	_ = filepath.Walk
}
