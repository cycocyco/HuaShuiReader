package cache

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CachedEntry 带标题指纹与元数据的持久化缓存条目
type CachedEntry struct {
	BookID       string    `json:"bookId"`
	ChapterIndex int       `json:"chapterIndex"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	CachedAt     time.Time `json:"cachedAt"`
}

type CacheStats struct {
	TotalFiles int64  `json:"totalFiles"`
	TotalBytes int64  `json:"totalBytes"`
	HumanSize  string `json:"humanSize"`
}

type Manager struct {
	mu       sync.RWMutex
	cacheDir string
}

func NewManager(cacheDir string) (*Manager, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{cacheDir: cacheDir}, nil
}

func (m *Manager) GetCacheDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cacheDir
}

// SwitchDir 安全切换缓存目录，支持旧缓存平滑迁移与原子验证
func (m *Manager) SwitchDir(newDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newDir = filepath.Clean(newDir)
	if newDir == m.cacheDir {
		return nil
	}

	// 1. 校验新目录可写性
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return fmt.Errorf("无法创建或访问新目录: %w", err)
	}

	testFile := filepath.Join(newDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("ok"), 0644); err != nil {
		return fmt.Errorf("新目录不可写: %w", err)
	}
	_ = os.Remove(testFile)

	// 2. 将旧目录现有缓存平滑复制迁移到新目录（防止丢弃已下载离线内容）
	oldDir := m.cacheDir
	if oldDir != "" {
		_ = copyDirContents(oldDir, newDir)
	}

	m.cacheDir = newDir
	return nil
}

func (m *Manager) getChapterPath(bookID string, chapterIndex int) string {
	h := md5.Sum([]byte(fmt.Sprintf("%s_%d", bookID, chapterIndex)))
	fileName := hex.EncodeToString(h[:]) + ".cache"
	subDir := filepath.Join(m.cacheDir, bookID)
	return filepath.Join(subDir, fileName)
}

// GetChapter 读取缓存章节，并对章节标题进行指纹比对校验，防止串章
func (m *Manager) GetChapter(bookID string, chapterIndex int, expectedTitle string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.getChapterPath(bookID, chapterIndex)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	var entry CachedEntry
	if err := json.Unmarshal(data, &entry); err == nil && entry.Content != "" {
		// 如果提供了期望标题且两者非空，验证标题匹配；若书源换源或换目录导致标题不符，则放弃命中
		if expectedTitle != "" && entry.Title != "" && !titleMatches(expectedTitle, entry.Title) {
			return "", false
		}
		return entry.Content, true
	}

	// 向下兼容旧纯文本缓存
	rawStr := string(data)
	if len(rawStr) > 0 {
		return rawStr, true
	}
	return "", false
}

// SaveChapter 原子写入带元数据签名的章节缓存
func (m *Manager) SaveChapter(bookID string, chapterIndex int, title string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("禁止空正文写入缓存")
	}

	entry := CachedEntry{
		BookID:       bookID,
		ChapterIndex: chapterIndex,
		Title:        title,
		Content:      content,
		CachedAt:     time.Now(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	path := m.getChapterPath(bookID, chapterIndex)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(path)
		return os.Rename(tmpPath, path)
	}
	return nil
}

// ClearBookCache 物理清理指定书籍的全部缓存
func (m *Manager) ClearBookCache(bookID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subDir := filepath.Join(m.cacheDir, bookID)
	return os.RemoveAll(subDir)
}

// ClearAllCache 一键清空所有已缓存章节
func (m *Manager) ClearAllCache() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		p := filepath.Join(m.cacheDir, entry.Name())
		_ = os.RemoveAll(p)
	}
	return nil
}

// GetCacheStats 统计当前缓存文件数量与总大小
func (m *Manager) GetCacheStats() CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stats CacheStats
	_ = filepath.Walk(m.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			stats.TotalFiles++
			stats.TotalBytes += info.Size()
		}
		return nil
	})

	stats.HumanSize = formatBytes(stats.TotalBytes)
	return stats
}

func titleMatches(t1, t2 string) bool {
	s1 := strings.TrimSpace(t1)
	s2 := strings.TrimSpace(t2)
	return s1 == s2 || strings.Contains(s1, s2) || strings.Contains(s2, s1)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			_ = os.MkdirAll(dstPath, 0755)
			_ = copyDirContents(srcPath, dstPath)
		} else {
			_ = copyFile(srcPath, dstPath)
		}
	}
	return nil
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
