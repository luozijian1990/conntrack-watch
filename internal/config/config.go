package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config 主配置结构
type Config struct {
	Ports      []uint16         `yaml:"ports"`
	Log        LogConfig        `yaml:"log"`
	Prometheus PrometheusConfig `yaml:"prometheus"`
	WebUI      WebUIConfig      `yaml:"web_ui"`
}

// LogConfig 日志配置
type LogConfig struct {
	Path       string `yaml:"path"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAgeDays int    `yaml:"max_age_days"`
	Compress   bool   `yaml:"compress"`
}

// PrometheusConfig Prometheus 配置
type PrometheusConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"`
}

// WebUIConfig Web UI 配置
type WebUIConfig struct {
	Enabled bool `yaml:"enabled"`
}

// Load 从 YAML 文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := &Config{
		// 默认值
		Ports: []uint16{80, 443},
		Log: LogConfig{
			Path:       "/var/log/nat-tracker/nat.log",
			MaxSizeMB:  100,
			MaxBackups: 10,
			MaxAgeDays: 7,
			Compress:   true,
		},
		Prometheus: PrometheusConfig{
			Enabled:    true,
			ListenAddr: ":9358",
		},
		WebUI: WebUIConfig{
			Enabled: false, // 默认关闭
		},
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}
