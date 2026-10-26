package library

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"huashuireader/internal/domain"
	"huashuireader/internal/source"
	_ "modernc.org/sqlite"
)

type Store struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "library.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// 强制开启 SQLite 外键支持与 WAL 模式，保障并发读写与级联一致性
	if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign_keys failed: %w", err)
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS books (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		author TEXT NOT NULL,
		source_type TEXT NOT NULL,
		source_id TEXT NOT NULL,
		book_url TEXT NOT NULL,
		cover_url TEXT,
		intro TEXT,
		last_chapter TEXT,
		total_chaps INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS progress (
		book_id TEXT PRIMARY KEY,
		chapter_id TEXT,
		chapter_index INTEGER DEFAULT 0,
		chapter_title TEXT,
		char_offset INTEGER DEFAULT 0,
		ratio REAL DEFAULT 0.0,
		updated_at DATETIME NOT NULL,
		FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS book_sources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		rule_json TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		weight INTEGER DEFAULT 0,
		updated_at DATETIME NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) SaveBook(b domain.Book) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO books (id, title, author, source_type, source_id, book_url, cover_url, intro, last_chapter, total_chaps, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title=excluded.title,
		author=excluded.author,
		last_chapter=excluded.last_chapter,
		total_chaps=excluded.total_chaps,
		updated_at=excluded.updated_at;
	`
	now := time.Now()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	b.UpdatedAt = now

	_, err := s.db.Exec(query, b.ID, b.Title, b.Author, b.SourceType, b.SourceID, b.BookURL, b.CoverURL, b.Intro, b.LastChapter, b.TotalChaps, b.CreatedAt, b.UpdatedAt)
	return err
}

func (s *Store) ListBooks() ([]domain.Book, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT id, title, author, source_type, source_id, book_url, cover_url, intro, last_chapter, total_chaps, created_at, updated_at FROM books ORDER BY updated_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Book
	for rows.Next() {
		var b domain.Book
		err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.SourceType, &b.SourceID, &b.BookURL, &b.CoverURL, &b.Intro, &b.LastChapter, &b.TotalChaps, &b.CreatedAt, &b.UpdatedAt)
		if err != nil {
			return nil, err
		}
		list = append(list, b)
	}
	return list, nil
}

// DeleteBook 安全删除书籍并级联清理进度，返回被删除的书籍信息
func (s *Store) DeleteBook(id string) (*domain.Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	book, _ := s.getBookInternal(id)

	// 双重保证：显式删除 progress，并依靠外键约束
	_, _ = s.db.Exec("DELETE FROM progress WHERE book_id = ?", id)
	_, err := s.db.Exec("DELETE FROM books WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return book, nil
}

func (s *Store) SaveProgress(p domain.Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO progress (book_id, chapter_id, chapter_index, chapter_title, char_offset, ratio, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(book_id) DO UPDATE SET
		chapter_id=excluded.chapter_id,
		chapter_index=excluded.chapter_index,
		chapter_title=excluded.chapter_title,
		char_offset=excluded.char_offset,
		ratio=excluded.ratio,
		updated_at=excluded.updated_at;
	`
	p.UpdatedAt = time.Now()
	_, err := s.db.Exec(query, p.BookID, p.ChapterID, p.ChapterIndex, p.ChapterTitle, p.CharOffset, p.Ratio, p.UpdatedAt)
	if err == nil {
		_, _ = s.db.Exec("UPDATE books SET updated_at = ? WHERE id = ?", p.UpdatedAt, p.BookID)
	}
	return err
}

func (s *Store) GetProgress(bookID string) (*domain.Progress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT book_id, chapter_id, chapter_index, chapter_title, char_offset, ratio, updated_at FROM progress WHERE book_id = ?`
	row := s.db.QueryRow(query, bookID)

	var p domain.Progress
	err := row.Scan(&p.BookID, &p.ChapterID, &p.ChapterIndex, &p.ChapterTitle, &p.CharOffset, &p.Ratio, &p.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetLastReadBookID 关联 books 表，严密排除已删除书籍的孤儿进度记录
func (s *Store) GetLastReadBookID() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var bookID string
	query := `
		SELECT p.book_id 
		FROM progress p
		INNER JOIN books b ON p.book_id = b.id
		ORDER BY p.updated_at DESC 
		LIMIT 1
	`
	err := s.db.QueryRow(query).Scan(&bookID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return bookID, nil
}

func (s *Store) getBookInternal(id string) (*domain.Book, error) {
	query := `SELECT id, title, author, source_type, source_id, book_url, cover_url, intro, last_chapter, total_chaps, created_at, updated_at FROM books WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var b domain.Book
	err := row.Scan(&b.ID, &b.Title, &b.Author, &b.SourceType, &b.SourceID, &b.BookURL, &b.CoverURL, &b.Intro, &b.LastChapter, &b.TotalChaps, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("book not found")
		}
		return nil, err
	}
	return &b, nil
}

func (s *Store) GetBook(id string) (*domain.Book, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getBookInternal(id)
}

// SaveSources 批量持久化书源规则，返回实际有效入库的数量
func (s *Store) SaveSources(sources []source.LegadoBookSource) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	query := `
	INSERT INTO book_sources (id, name, url, rule_json, enabled, weight, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		url=excluded.url,
		rule_json=excluded.rule_json,
		enabled=excluded.enabled,
		updated_at=excluded.updated_at;
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now()
	validCount := 0
	for _, src := range sources {
		if src.BookSourceName == "" {
			continue
		}
		h := md5.Sum([]byte(src.BookSourceName + src.BookSourceURL))
		id := hex.EncodeToString(h[:])

		raw, _ := json.Marshal(src)
		enabledInt := 1
		if !src.Enabled {
			enabledInt = 0
		}
		_, err := stmt.Exec(id, src.BookSourceName, src.BookSourceURL, string(raw), enabledInt, 0, now)
		if err != nil {
			return 0, err
		}
		validCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return validCount, nil
}

// GetAllSources 获取所有已存储的书源规则
func (s *Store) GetAllSources() ([]source.LegadoBookSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT rule_json, enabled FROM book_sources ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []source.LegadoBookSource
	for rows.Next() {
		var raw string
		var enabledInt int
		if err := rows.Scan(&raw, &enabledInt); err == nil {
			var src source.LegadoBookSource
			if json.Unmarshal([]byte(raw), &src) == nil {
				src.Enabled = (enabledInt == 1)
				list = append(list, src)
			}
		}
	}
	return list, nil
}

// GetSourceByURL 根据 URL 查找对应的书源规则
func (s *Store) GetSourceByURL(sourceURL string) (*source.LegadoBookSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var raw string
	var enabledInt int
	err := s.db.QueryRow("SELECT rule_json, enabled FROM book_sources WHERE url = ? LIMIT 1", sourceURL).Scan(&raw, &enabledInt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	var src source.LegadoBookSource
	if err := json.Unmarshal([]byte(raw), &src); err != nil {
		return nil, err
	}
	src.Enabled = (enabledInt == 1)
	return &src, nil
}
