package config

import (
	"flag"
	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	ReqbouncerHost string `koanf:"reqbouncer_host" validate:"required"`
	GithubClientId string `koanf:"github_client_id" validate:"required"`
}

type BuntConfig struct {
	Path string `koanf:"path" validate:"required"`
}

var k = koanf.New(".")

func K() *koanf.Koanf {
	return k
}

const maxAttempts = 10

func repeatString(s string, count int) string {
	var result string
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}

func findConfigFile(fileName string) (bool, string) {
	for i := 0; i < maxAttempts; i++ {
		path := filepath.Join(repeatString("../", i), fileName)
		if _, err := os.Stat(path); err == nil {
			return true, path
		}
	}
	return false, ""
}

func IsTest() bool {
	if flag.Lookup("test.v") == nil {
		return false
	} else {
		return true
	}
}

func Init() (*Config, error) {
	k := k
	k.Load(env.Provider("", ".", func(s string) string {
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, "")), "__", ".", -1)
	}), nil)
	slog.Debug("Loaded environment variables")

	localToml, localTomlPath := findConfigFile("config.toml")

	if localToml {
		err := k.Load(file.Provider(localTomlPath), toml.Parser())
		if err != nil {
			return nil, err
		}
		slog.Debug("Loaded local config from: " + localTomlPath)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}

	v := validator.New()
	if err := v.Struct(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
