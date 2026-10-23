package configs

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ALLOW_ORIGINS", "")
	t.Setenv("MAX_BODY_BYTES", "")
	t.Setenv("MIGRATE_ON_START", "")

	cfg := Load()

	if cfg.Port != "8084" {
		t.Errorf("Port = %q, want 8084", cfg.Port)
	}
	if cfg.MaxBodyBytes != 1048576 {
		t.Errorf("MaxBodyBytes = %d, want 1048576", cfg.MaxBodyBytes)
	}
	if !cfg.MigrateOnStart {
		t.Error("MigrateOnStart = false, want true by default")
	}
	wantOrigins := []string{"http://localhost:3000", "http://localhost:3008"}
	if len(cfg.AllowOrigins) != len(wantOrigins) {
		t.Fatalf("AllowOrigins = %v, want %v", cfg.AllowOrigins, wantOrigins)
	}
	for i, o := range wantOrigins {
		if cfg.AllowOrigins[i] != o {
			t.Errorf("AllowOrigins[%d] = %q, want %q", i, cfg.AllowOrigins[i], o)
		}
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://u:p@h:5432/db")
	t.Setenv("ALLOW_ORIGINS", "http://a.test, http://b.test")
	t.Setenv("MAX_BODY_BYTES", "2048")
	t.Setenv("MIGRATE_ON_START", "false")

	cfg := Load()

	if cfg.Port != "9000" {
		t.Errorf("Port = %q, want 9000", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://u:p@h:5432/db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.MaxBodyBytes != 2048 {
		t.Errorf("MaxBodyBytes = %d, want 2048", cfg.MaxBodyBytes)
	}
	if cfg.MigrateOnStart {
		t.Error("MigrateOnStart = true, want false")
	}
	if len(cfg.AllowOrigins) != 2 || cfg.AllowOrigins[0] != "http://a.test" || cfg.AllowOrigins[1] != "http://b.test" {
		t.Errorf("AllowOrigins = %v, want trimmed pair", cfg.AllowOrigins)
	}
}

func TestLoadIgnoresMalformedBodyLimit(t *testing.T) {
	t.Setenv("MAX_BODY_BYTES", "lots")

	if got := Load().MaxBodyBytes; got != 1048576 {
		t.Errorf("MaxBodyBytes = %d, want default 1048576 on malformed input", got)
	}
}

func TestMigrateOnStartAcceptsBoolSpellings(t *testing.T) {
	cases := map[string]bool{"true": true, "TRUE": true, "1": true, "false": false, "0": false, "FALSE": false, "nope": true}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("MIGRATE_ON_START", raw)
			if got := Load().MigrateOnStart; got != want {
				t.Errorf("MIGRATE_ON_START=%q → %v, want %v", raw, got, want)
			}
		})
	}
}

const defaultImportLimit = 53477376 // 51 MiB: a 50 MiB zip plus multipart overhead

func TestLoadImportLimitDefaultsTo51MiB(t *testing.T) { // S01-6
	t.Setenv("MAX_IMPORT_BYTES", "")

	if got := Load().MaxImportBytes; got != defaultImportLimit {
		t.Errorf("MaxImportBytes = %d, want %d", got, defaultImportLimit)
	}
}

func TestLoadImportLimitCanBeOverridden(t *testing.T) { // S01-6
	t.Setenv("MAX_IMPORT_BYTES", "2048")

	if got := Load().MaxImportBytes; got != 2048 {
		t.Errorf("MaxImportBytes = %d, want 2048", got)
	}
}

func TestLoadIgnoresMalformedImportLimit(t *testing.T) { // S01-6
	t.Setenv("MAX_IMPORT_BYTES", "huge")

	if got := Load().MaxImportBytes; got != defaultImportLimit {
		t.Errorf("MaxImportBytes = %d, want default %d on malformed input", got, defaultImportLimit)
	}
}

func TestImportLimitIsIndependentOfBodyLimit(t *testing.T) { // S01-6
	t.Setenv("MAX_BODY_BYTES", "2048")
	t.Setenv("MAX_IMPORT_BYTES", "")

	cfg := Load()
	if cfg.MaxBodyBytes != 2048 || cfg.MaxImportBytes != defaultImportLimit {
		t.Errorf("MaxBodyBytes = %d, MaxImportBytes = %d; want 2048 and %d", cfg.MaxBodyBytes, cfg.MaxImportBytes, defaultImportLimit)
	}
}
