package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"huashuireader/internal/cache"
	"huashuireader/internal/domain"
	"huashuireader/internal/library"
	"huashuireader/internal/local"
	"huashuireader/internal/platform/windows"
	"huashuireader/internal/reader"
	"huashuireader/internal/settings"
	"huashuireader/internal/source"
)

// App Wails v3 后端主控制器服务
type App struct {
	app             *application.App
	mainWindow      *application.WebviewWindow
	settingsService *settings.Service
	store           *library.Store
	localParser     *local.Parser
	cacheMgr        *cache.Manager
	sourceEngine    *source.Engine
	readerService   *reader.Service
	appDataDir      string
	libraryBooksDir string
	mu              sync.Mutex
	activeBookID    string
}

func NewApp() *App {
	exePath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(exePath)
	}

	appDataDir := filepath.Join(baseDir, "data")
	libraryBooksDir := filepath.Join(appDataDir, "books")
	cacheDir := filepath.Join(appDataDir, "cache")

	settingsSvc, err := settings.NewService(appDataDir)
	if err != nil {
		fmt.Printf("Init settings notice: %v\n", err)
	}

	cfg := settingsSvc.Get()
	if cfg.CacheDir != "" {
		cacheDir = cfg.CacheDir
	}

	store, err := library.NewStore(appDataDir)
	if err != nil {
		fmt.Printf("Init store notice: %v\n", err)
	}

	cacheMgr, err := cache.NewManager(cacheDir)
	if err != nil {
		fmt.Printf("Init cache manager notice: %v\n", err)
	}

	localParser := local.NewParser()
	sourceEngine := source.NewEngine()
	readerSvc := reader.NewService(store, localParser, cacheMgr, sourceEngine)

	return &App{
		settingsService: settingsSvc,
		store:           store,
		localParser:     localParser,
		cacheMgr:        cacheMgr,
		sourceEngine:    sourceEngine,
		readerService:   readerSvc,
		appDataDir:      appDataDir,
		libraryBooksDir: libraryBooksDir,
	}
}

// AttachRuntime 挂载 Wails v3 实例与主窗口句柄
func (a *App) AttachRuntime(app *application.App, win *application.WebviewWindow) {
	a.app = app
	a.mainWindow = win
}

// SaveWindowState 保存当前窗口几何状态
func (a *App) SaveWindowState() {
	if a.mainWindow == nil {
		return
	}
	x, y := a.mainWindow.Position()
	w, h := a.mainWindow.Size()
	isMax := a.mainWindow.IsMaximised()

	cfg := a.settingsService.Get()
	if !isMax && w >= 280 && h >= 360 {
		cfg.WindowWidth = w
		cfg.WindowHeight = h
		cfg.WindowX = x
		cfg.WindowY = y
	}
	cfg.IsMaximized = isMax
	_ = a.settingsService.Save(cfg)
}

// ================= 窗口控制 API =================

func (a *App) WindowMinimize() {
	if a.mainWindow != nil {
		a.mainWindow.Minimise()
	}
}

func (a *App) WindowToggleMaximize() {
	if a.mainWindow != nil {
		a.mainWindow.ToggleMaximise()
	}
}

func (a *App) WindowClose() {
	a.SaveWindowState()
	if a.app != nil {
		a.app.Quit()
	}
}

// PickScreenColor 进入屏幕吸管模式 (支持跨窗口吸色)
func (a *App) PickScreenColor() (string, error) {
	return windows.StartScreenColorSampling(15)
}

// ================= 设置与缓存管理 API =================

func (a *App) GetSettings() domain.AppSettings {
	return a.settingsService.Get()
}

func (a *App) SaveSettings(cfg domain.AppSettings) error {
	return a.settingsService.Save(cfg)
}

// SelectCacheDir 使用 Wails v3 原生对话框选择新缓存目录
func (a *App) SelectCacheDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("app uninitialized")
	}

	dialog := a.app.Dialog.OpenFile()
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)
	dialog.SetTitle("选择小说缓存存储目录")

	selected, err := dialog.PromptForSingleSelection()
	if err != nil || selected == "" {
		return "", err
	}
	if err := a.SwitchCacheDir(selected); err != nil {
		return "", err
	}
	return selected, nil
}

func (a *App) SwitchCacheDir(newDir string) error {
	if a.cacheMgr == nil {
		return fmt.Errorf("cache manager uninitialized")
	}
	if err := a.cacheMgr.SwitchDir(newDir); err != nil {
		return err
	}
	cfg := a.settingsService.Get()
	cfg.CacheDir = newDir
	return a.settingsService.Save(cfg)
}

func (a *App) GetCacheStats() cache.CacheStats {
	if a.cacheMgr == nil {
		return cache.CacheStats{HumanSize: "0 B"}
	}
	return a.cacheMgr.GetCacheStats()
}

func (a *App) ClearAllCache() error {
	if a.cacheMgr == nil {
		return nil
	}
	return a.cacheMgr.ClearAllCache()
}

// ================= 书架与阅读 API =================

func (a *App) ListBooks() ([]domain.Book, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store uninitialized")
	}
	return a.store.ListBooks()
}

// DeleteBook 安全彻底删除书籍（数据库级联、本地 txt/md 副本、章节缓存目录三位一体清除）
func (a *App) DeleteBook(bookID string) error {
	if a.store == nil {
		return fmt.Errorf("store uninitialized")
	}

	book, err := a.store.DeleteBook(bookID)
	if err != nil {
		return err
	}

	if book != nil && (book.SourceType == "local_txt" || book.SourceType == "local_md") {
		if strings.HasPrefix(filepath.Clean(book.BookURL), filepath.Clean(a.libraryBooksDir)) {
			_ = os.Remove(book.BookURL)
		}
	}

	if a.cacheMgr != nil {
		_ = a.cacheMgr.ClearBookCache(bookID)
	}

	return nil
}

func (a *App) ImportLocalFile() (*domain.Book, error) {
	if a.app == nil {
		return nil, fmt.Errorf("app uninitialized")
	}

	dialog := a.app.Dialog.OpenFile()
	dialog.SetTitle("选择本地小说文件")
	dialog.AddFilter("小说文本文件 (*.txt;*.md)", "*.txt;*.md;*.markdown")

	selection, err := dialog.PromptForSingleSelection()
	if err != nil || selection == "" {
		return nil, err
	}

	book, err := a.localParser.ImportBook(selection, a.libraryBooksDir)
	if err != nil {
		return nil, err
	}

	if err := a.store.SaveBook(book); err != nil {
		return nil, err
	}

	return &book, nil
}

func (a *App) SaveBook(book domain.Book) error {
	if a.store == nil {
		return fmt.Errorf("store uninitialized")
	}
	return a.store.SaveBook(book)
}

func (a *App) GetLastReadBookID() (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("store uninitialized")
	}
	return a.store.GetLastReadBookID()
}

func (a *App) GetProgress(bookID string) (*domain.Progress, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store uninitialized")
	}
	return a.store.GetProgress(bookID)
}

func (a *App) SaveProgress(progress domain.Progress) error {
	if a.store == nil {
		return fmt.Errorf("store uninitialized")
	}
	a.mu.Lock()
	a.activeBookID = progress.BookID
	a.mu.Unlock()
	return a.store.SaveProgress(progress)
}

func (a *App) GetChapterList(bookID string) ([]domain.Chapter, error) {
	if a.readerService == nil {
		return nil, fmt.Errorf("reader uninitialized")
	}
	return a.readerService.GetChapterList(bookID)
}

func (a *App) ReadChapter(bookID string, chapterIndex int) (*domain.ChapterContent, error) {
	if a.readerService == nil {
		return nil, fmt.Errorf("reader uninitialized")
	}
	a.mu.Lock()
	a.activeBookID = bookID
	a.mu.Unlock()
	return a.readerService.ReadChapter(bookID, chapterIndex)
}

// ================= 在线书源 API =================

func (a *App) ImportSourcesFromFile() (int, error) {
	if a.app == nil {
		return 0, fmt.Errorf("app uninitialized")
	}

	dialog := a.app.Dialog.OpenFile()
	dialog.SetTitle("选择本地书源 JSON 文件")
	dialog.AddFilter("书源文件 (*.json)", "*.json")

	selection, err := dialog.PromptForSingleSelection()
	if err != nil || selection == "" {
		return 0, err
	}

	list, err := a.sourceEngine.ParseSourcesFromFile(selection)
	if err != nil {
		return 0, err
	}
	return a.store.SaveSources(list)
}

func (a *App) ImportSourcesFromURL(url string) (int, error) {
	list, err := a.sourceEngine.FetchSourcesFromURL(url)
	if err != nil {
		return 0, err
	}
	return a.store.SaveSources(list)
}

func (a *App) ImportSourcesFromJSON(jsonText string) (int, error) {
	list, err := a.sourceEngine.ParseSourcesFromJSON(jsonText)
	if err != nil {
		return 0, err
	}
	return a.store.SaveSources(list)
}

func (a *App) GetSourcesCount() int {
	if a.store == nil {
		return 0
	}
	sources, err := a.store.GetAllSources()
	if err != nil {
		return 0
	}
	return len(sources)
}

func (a *App) SearchBooks(keyword string) []source.SearchResultItem {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || a.store == nil {
		return nil
	}
	sources, err := a.store.GetAllSources()
	if err != nil || len(sources) == 0 {
		return nil
	}
	return a.sourceEngine.SearchBooksAcrossSources(sources, keyword)
}
