package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsService_SaveAndRestore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "settings_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	svc, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	cfg := svc.Get()
	cfg.FontSize = 18
	cfg.FontFamily = "KaiTi"
	cfg.TextColor = "#123456"

	if err := svc.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	// 再次保存以将修改后的数据同步写入 .bak
	if err := svc.Save(cfg); err != nil {
		t.Fatalf("Save 2 failed: %v", err)
	}

	// 重新加载验证
	svc2, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("NewService 2 failed: %v", err)
	}
	loaded := svc2.Get()
	if loaded.FontSize != 18 || loaded.FontFamily != "KaiTi" || loaded.TextColor != "#123456" {
		t.Fatalf("Loaded settings mismatch: %+v", loaded)
	}

	// 模拟主文件损坏，验证从 bak 恢复
	cfgPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(cfgPath, []byte("broken json{{"), 0644); err != nil {
		t.Fatalf("failed to corrupt config: %v", err)
	}

	svc3, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("NewService 3 failed: %v", err)
	}
	restored := svc3.Get()
	if restored.FontSize != 18 {
		t.Fatalf("Failed to restore from backup: %+v", restored)
	}
}
