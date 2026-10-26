package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	appCore := NewApp()
	cfg := appCore.GetSettings()

	winWidth := cfg.WindowWidth
	if winWidth < 280 {
		winWidth = 340
	}
	winHeight := cfg.WindowHeight
	if winHeight < 360 {
		winHeight = 480
	}

	exePath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(exePath)
	}
	appDataDir := filepath.Join(baseDir, "data")
	webviewUserDataDir := filepath.Join(appDataDir, "webview2")

	app := application.New(application.Options{
		Name:        "HuaShuiReader",
		Description: "划水纪 - 摸鱼小说阅读器",
		Services: []application.Service{
			application.NewService(appCore),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewUserDataDir, // 彻底杜绝 %APPDATA% 痕迹，实现 100% 便携
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "HuaShuiReader_SingleInstance_Mutex_Lock",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if appCore.mainWindow != nil {
					appCore.mainWindow.UnMinimise()
					appCore.mainWindow.Show()
					appCore.mainWindow.Focus()
				}
			},
		},
		OnShutdown: func() {
			appCore.SaveWindowState()
		},
	})

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "划水纪 - HuaShui Reader",
		Width:            winWidth,
		Height:           winHeight,
		MinWidth:         280,
		MinHeight:        360,
		Frameless:        true, // 无原生厚边框
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/",
	})

	appCore.AttachRuntime(app, mainWindow)

	if cfg.WindowX != -1 || cfg.WindowY != -1 {
		mainWindow.SetPosition(cfg.WindowX, cfg.WindowY)
	} else {
		mainWindow.Center()
	}
	if cfg.IsMaximized {
		mainWindow.Maximise()
	}

	err = app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
