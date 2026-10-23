package configs

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration, resolved once from environment
// variables with working local defaults (12-factor: env only).
type Config struct {
	Port           string
	DatabaseURL    string
	AllowOrigins   []string
	MaxBodyBytes   int64
	MaxImportBytes int64
	MigrateOnStart bool
	MediaDir       string
}

func Load() Config {
	return Config{
		Port:           envOr("PORT", "8084"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		AllowOrigins:   splitOrigins(envOr("ALLOW_ORIGINS", "http://localhost:3000,http://localhost:3008")),
		MaxBodyBytes:   envInt64Or("MAX_BODY_BYTES", 1048576),
		MaxImportBytes: envInt64Or("MAX_IMPORT_BYTES", 53477376), // 51 MiB: a 50 MiB zip plus multipart overhead
		MigrateOnStart: envBoolOr("MIGRATE_ON_START", true),
		MediaDir:       envOr("MEDIA_DIR", "/data/media"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	b, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return b
}

func envInt64Or(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func splitOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}
