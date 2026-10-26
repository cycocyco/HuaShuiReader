package reader

import (
	"fmt"
	"huashuireader/internal/cache"
	"huashuireader/internal/domain"
	"huashuireader/internal/library"
	"huashuireader/internal/local"
	"huashuireader/internal/source"
	"sync"
)

type Service struct {
	store        *library.Store
	localParser  *local.Parser
	cacheMgr     *cache.Manager
	sourceEngine *source.Engine

	// 本地书籍章节偏移缓存: bookID -> []local.ChapterOffset
	offsetCache map[string][]local.ChapterOffset
	// 在线书籍目录缓存: bookID -> []domain.Chapter
	onlineChapterCache map[string][]domain.Chapter
	mu                 sync.RWMutex
}

func NewService(store *library.Store, localParser *local.Parser, cacheMgr *cache.Manager, sourceEngine *source.Engine) *Service {
	return &Service{
		store:              store,
		localParser:        localParser,
		cacheMgr:           cacheMgr,
		sourceEngine:       sourceEngine,
		offsetCache:        make(map[string][]local.ChapterOffset),
		onlineChapterCache: make(map[string][]domain.Chapter),
	}
}

// GetChapterList 获取书籍目录（支持本地与在线抓取）
func (s *Service) GetChapterList(bookID string) ([]domain.Chapter, error) {
	book, err := s.store.GetBook(bookID)
	if err != nil {
		return nil, err
	}

	// 1. 本地书籍
	if book.SourceType == "local_txt" || book.SourceType == "local_md" {
		s.mu.Lock()
		defer s.mu.Unlock()

		offsets, ok := s.offsetCache[bookID]
		if !ok {
			enc, _ := s.localParser.DetectEncoding(book.BookURL)
			scanned, err := s.localParser.ScanChapters(book.BookURL, enc)
			if err != nil {
				return nil, err
			}
			offsets = scanned
			s.offsetCache[bookID] = offsets
		}

		var chaps []domain.Chapter
		for i, o := range offsets {
			chaps = append(chaps, domain.Chapter{
				ID:       fmt.Sprintf("%s_%d", bookID, i),
				BookID:   bookID,
				Index:    i,
				Title:    o.Title,
				IsCached: true,
			})
		}
		return chaps, nil
	}

	// 2. 在线书籍
	s.mu.RLock()
	onlineChaps, ok := s.onlineChapterCache[bookID]
	s.mu.RUnlock()
	if ok && len(onlineChaps) > 0 {
		return onlineChaps, nil
	}

	// 匹配绑定的 Legado 书源规则
	var srcRule *source.LegadoBookSource
	if book.SourceID != "" {
		srcRule, _ = s.store.GetSourceByURL(book.SourceID)
	}

	// 从网络根据书源规则抓取目录
	fetched, err := s.sourceEngine.FetchChaptersWithSource(book.BookURL, bookID, srcRule)
	if err != nil {
		return nil, fmt.Errorf("在线目录加载失败: %w", err)
	}

	s.mu.Lock()
	s.onlineChapterCache[bookID] = fetched
	s.mu.Unlock()

	return fetched, nil
}

// ReadChapter 读取指定章节正文，优先走本地切片与本地缓存，在线书缺省时自动实时网络抓取
func (s *Service) ReadChapter(bookID string, chapterIndex int) (*domain.ChapterContent, error) {
	book, err := s.store.GetBook(bookID)
	if err != nil {
		return nil, err
	}

	// 1. 本地书
	if book.SourceType == "local_txt" || book.SourceType == "local_md" {
		s.mu.Lock()
		offsets, ok := s.offsetCache[bookID]
		if !ok {
			enc, _ := s.localParser.DetectEncoding(book.BookURL)
			scanned, err := s.localParser.ScanChapters(book.BookURL, enc)
			if err != nil {
				s.mu.Unlock()
				return nil, err
			}
			offsets = scanned
			s.offsetCache[bookID] = offsets
		}
		s.mu.Unlock()

		if chapterIndex < 0 || chapterIndex >= len(offsets) {
			return nil, fmt.Errorf("章节索引越界: %d", chapterIndex)
		}

		cur := offsets[chapterIndex]
		enc, _ := s.localParser.DetectEncoding(book.BookURL)
		text, err := s.localParser.ReadChapterRange(book.BookURL, enc, cur.RuneStart, cur.RuneEnd, cur.Title)
		if err != nil {
			return nil, err
		}

		return &domain.ChapterContent{
			BookID:       bookID,
			ChapterIndex: chapterIndex,
			Title:        cur.Title,
			Content:      text,
			HasNext:      chapterIndex < len(offsets)-1,
			HasPrev:      chapterIndex > 0,
		}, nil
	}

	// 2. 在线书 - 首先检查本地磁盘缓存
	chaps, _ := s.GetChapterList(bookID)
	var expectedTitle string
	if len(chaps) > chapterIndex && chapterIndex >= 0 {
		expectedTitle = chaps[chapterIndex].Title
	}

	if cached, ok := s.cacheMgr.GetChapter(bookID, chapterIndex, expectedTitle); ok {
		title := expectedTitle
		if title == "" {
			title = fmt.Sprintf("第 %d 章", chapterIndex+1)
		}
		hasNext := len(chaps) > 0 && chapterIndex < len(chaps)-1

		return &domain.ChapterContent{
			BookID:       bookID,
			ChapterIndex: chapterIndex,
			Title:        title,
			Content:      cached,
			HasNext:      hasNext,
			HasPrev:      chapterIndex > 0,
		}, nil
	}

	// 缓存未命中：在线实时抓取正文
	if len(chaps) == 0 {
		var err error
		chaps, err = s.GetChapterList(bookID)
		if err != nil {
			return nil, err
		}
	}
	if chapterIndex < 0 || chapterIndex >= len(chaps) {
		return nil, fmt.Errorf("在线章节超出范围: %d", chapterIndex)
	}

	curChap := chaps[chapterIndex]
	if curChap.URL == "" {
		return nil, fmt.Errorf("该章节没有有效链接")
	}

	// 匹配绑定的 Legado 书源规则
	var srcRule *source.LegadoBookSource
	if book.SourceID != "" {
		srcRule, _ = s.store.GetSourceByURL(book.SourceID)
	}

	text, err := s.sourceEngine.FetchContentWithSource(curChap.URL, curChap.Title, srcRule)
	if err != nil {
		return nil, fmt.Errorf("抓取正文失败: %w", err)
	}

	// 写入本地持久缓存（带标题签名）
	_ = s.cacheMgr.SaveChapter(bookID, chapterIndex, curChap.Title, text)

	return &domain.ChapterContent{
		BookID:       bookID,
		ChapterIndex: chapterIndex,
		Title:        curChap.Title,
		Content:      text,
		HasNext:      chapterIndex < len(chaps)-1,
		HasPrev:      chapterIndex > 0,
	}, nil
}
