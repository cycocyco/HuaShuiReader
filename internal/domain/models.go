package domain

import (
	"time"
)

// AppSettings 软件配置实体
type AppSettings struct {
	// 窗口状态
	WindowWidth  int  `json:"windowWidth"`
	WindowHeight int  `json:"windowHeight"`
	WindowX      int  `json:"windowX"`
	WindowY      int  `json:"windowY"`
	IsMaximized  bool `json:"isMaximized"`

	// 外观主题
	ThemeName       string  `json:"themeName"`       // dark, light, eye, custom
	BackgroundColor string  `json:"backgroundColor"` // HEX
	PanelColor      string  `json:"panelColor"`      // HEX
	TitleBarColor   string  `json:"titleBarColor"`   // HEX
	TextColor       string  `json:"textColor"`       // HEX
	TextDimColor    string  `json:"textDimColor"`    // HEX
	AccentColor     string  `json:"accentColor"`     // HEX
	FontFamily      string  `json:"fontFamily"`      // 系统字体名
	FontSize        int     `json:"fontSize"`        // 像素字号
	LineHeight      float64 `json:"lineHeight"`      // 1.2 - 2.0

	// 快捷键
	MinimizeKey string `json:"minimizeKey"` // 默认 "Ctrl + Alt + H"

	// 缓存路径
	CacheDir string `json:"cacheDir"`
}

// DefaultSettings 默认配置
func DefaultSettings() AppSettings {
	return AppSettings{
		WindowWidth:     340,
		WindowHeight:    480,
		WindowX:         -1,
		WindowY:         -1,
		IsMaximized:     false,
		ThemeName:       "custom",
		BackgroundColor: "#1E1E1E",
		PanelColor:      "#252526",
		TitleBarColor:   "#1E1E1E",
		TextColor:       "#D4D4D4",
		TextDimColor:    "#808080",
		AccentColor:     "#5865F2",
		FontFamily:      "Microsoft YaHei",
		FontSize:        14,
		LineHeight:      1.5,
		MinimizeKey:     "Ctrl + Alt + H",
		CacheDir:        "",
	}
}

// Book 统一书籍实体
type Book struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	SourceType  string    `json:"sourceType"` // local_txt, local_md, online
	SourceID    string    `json:"sourceId"`   // 书源ID或本地空
	BookURL     string    `json:"bookUrl"`    // 在线URL或本地绝对路径
	CoverURL    string    `json:"coverUrl"`
	Intro       string    `json:"intro"`
	LastChapter string    `json:"lastChapter"`
	TotalChaps  int       `json:"totalChaps"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Chapter 章节元数据
type Chapter struct {
	ID       string `json:"id"`
	BookID   string `json:"bookId"`
	Index    int    `json:"index"`
	Title    string `json:"title"`
	URL      string `json:"url"` // 在线章URL或本地偏移指示
	IsCached bool   `json:"isCached"`
}

// Progress 阅读进度
type Progress struct {
	BookID       string    `json:"bookId"`
	ChapterID    string    `json:"chapterId"`
	ChapterIndex int       `json:"chapterIndex"`
	ChapterTitle string    `json:"chapterTitle"`
	CharOffset   int       `json:"charOffset"` // 字符偏移量
	Ratio        float64   `json:"ratio"`      // 0.0 - 1.0 降级进度
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ChapterContent 章节正文数据
type ChapterContent struct {
	BookID       string `json:"bookId"`
	ChapterIndex int    `json:"chapterIndex"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	HasNext      bool   `json:"hasNext"`
	HasPrev      bool   `json:"hasPrev"`
}
