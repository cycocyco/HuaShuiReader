package settings

import (
	"encoding/json"
	"fmt"
	"huashuireader/internal/domain"
	"os"
	"path/filepath"
	"sync"
)

type Service struct {
	mu       sync.RWMutex
	filePath string
	bakPath  string
	settings domain.AppSettings
}

func NewService(appDataDir string) (*Service, error) {
	if err := os.MkdirAll(appDataDir, 0755); err != nil {
		// 即使创建目录受阻，返回默认内存配置服务，确保不解引用 nil
		return &Service{
			settings: domain.DefaultSettings(),
		}, fmt.Errorf("mkdir appDataDir failed: %w", err)
	}

	cfgPath := filepath.Join(appDataDir, "config.json")
	bakPath := filepath.Join(appDataDir, "config.json.bak")

	s := &Service{
		filePath: cfgPath,
		bakPath:  bakPath,
		settings: domain.DefaultSettings(),
	}

	if err := s.load(); err != nil {
		// 主配置损坏，尝试从备份恢复
		if bakErr := s.loadFrom(bakPath); bakErr != nil {
			s.settings = domain.DefaultSettings()
			_ = s.Save(s.settings)
		} else {
			// 备份有效，从备份回写主文件
			_ = s.Save(s.settings)
		}
	}

	return s, nil
}

func (s *Service) load() error {
	return s.loadFrom(s.filePath)
}

func (s *Service) loadFrom(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var loaded domain.AppSettings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = loaded
	s.mu.Unlock()
	return nil
}

func (s *Service) Get() domain.AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Save 原子写入并备份，包含 fsync 刷盘与故障回滚
func (s *Service) Save(newSettings domain.AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filePath == "" {
		s.settings = newSettings
		return nil
	}

	data, err := json.MarshalIndent(newSettings, "", "  ")
	if err != nil {
		return err
	}

	// 1. 写入临时文件并强行 fsync 刷盘，杜绝掉电空配置
	tmpPath := s.filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create tmp config failed: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write tmp config failed: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync tmp config failed: %w", err)
	}
	_ = f.Close()

	// 2. 原主配置备份为 .bak
	if _, err := os.Stat(s.filePath); err == nil {
		if oldData, rErr := os.ReadFile(s.filePath); rErr == nil && len(oldData) > 0 {
			_ = os.WriteFile(s.bakPath, oldData, 0644)
		}
	}

	// 3. 原子重命名覆盖（Windows 平台兼容处理）
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		_ = os.Remove(s.filePath)
		if err := os.Rename(tmpPath, s.filePath); err != nil {
			// 重命名失败尝试从 .bak 救回
			if bakData, bErr := os.ReadFile(s.bakPath); bErr == nil {
				_ = os.WriteFile(s.filePath, bakData, 0644)
			}
			return fmt.Errorf("atomic rename failed, rolled back: %w", err)
		}
	}

	s.settings = newSettings
	return nil
}
