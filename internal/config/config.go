package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 保存服务运行所需的环境配置。
type Config struct {
	Addr           string
	DBPath         string
	MaxPayload     int64
	MaxRetries     int
	RetryBase      time.Duration
	ForwardTimeout time.Duration
	AllowPrivate   bool
	AllowLoopback  bool
	ReplayWindow   time.Duration
}

// Load 读取并校验环境变量。
func Load() (*Config, error) {
	cfg := &Config{
		Addr:           getEnv("HTTP_ADDR", ":8080"),
		DBPath:         getEnv("DB_PATH", "./data/router.db"),
		MaxPayload:     getEnvInt64("MAX_PAYLOAD", 1<<20),
		MaxRetries:     getEnvInt("MAX_RETRIES", 5),
		RetryBase:      time.Duration(getEnvInt64("RETRY_BASE_MS", 1000)) * time.Millisecond,
		ForwardTimeout: time.Duration(getEnvInt64("FORWARD_TIMEOUT_MS", 5000)) * time.Millisecond,
		AllowPrivate:   getEnvBool("ALLOW_PRIVATE_TARGET", false),
		AllowLoopback:  getEnvBool("ALLOW_LOOPBACK_TARGET", false),
		ReplayWindow:   time.Duration(getEnvInt64("REPLAY_WINDOW_SEC", 300)) * time.Second,
	}
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("DB_PATH must not be empty")
	}
	if cfg.MaxPayload <= 0 {
		return nil, fmt.Errorf("MAX_PAYLOAD must be positive")
	}
	if cfg.MaxRetries <= 0 {
		return nil, fmt.Errorf("MAX_RETRIES must be positive")
	}
	if cfg.ForwardTimeout <= 0 {
		return nil, fmt.Errorf("FORWARD_TIMEOUT_MS must be positive")
	}
	if cfg.ReplayWindow <= 0 {
		return nil, fmt.Errorf("REPLAY_WINDOW_SEC must be positive")
	}
	return cfg, nil
}

func getEnv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt64(k string, def int64) int64 {
	v := getEnv(k, "")
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func getEnvInt(k string, def int) int {
	return int(getEnvInt64(k, int64(def)))
}

func getEnvBool(k string, def bool) bool {
	v := getEnv(k, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
