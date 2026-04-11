package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const defaultPort = 9528

// Config 统揽应用程序的配置文件参数
type Config struct {
	Server struct {
		Port   int    `yaml:"port"`
		APIKey string `yaml:"api_key"`
	} `yaml:"server"`
	Backends struct {
		Gemini struct {
			Enabled       bool     `yaml:"enabled"`
			PreloadModels []string `yaml:"preload_models"`
		} `yaml:"gemini"`
	} `yaml:"backends"`
}

// LoadConfig 从指定的文件路径解析应用程序配置
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = defaultPort
	}

	return &cfg, nil
}
