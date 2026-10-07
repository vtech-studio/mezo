package main

import "testing"

func TestVercelRequiresProductionOriginAndUsesPort(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("VERCEL_ENV", "production")
	t.Setenv("PORT", "8080")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("DATABASE_ADMIN_URL", "postgres://example:password@example.test/postgres")
	t.Setenv("APP_SECRET", "a-test-secret-with-at-least-32-characters")
	t.Setenv("ADMIN_EMAIL", "admin@example.test")
	t.Setenv("ADMIN_PASSWORD", "a-test-password-only")
	t.Setenv("APP_ORIGIN", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("Vercel must require the public production origin")
	}
	t.Setenv("APP_ORIGIN", "http://example.test")
	if _, err := loadConfig(); err == nil {
		t.Fatal("Vercel must require HTTPS")
	}
	t.Setenv("APP_ORIGIN", "https://example.test/")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" || cfg.Origin != "https://example.test" || !cfg.Secure || !cfg.Serverless {
		t.Fatalf("unexpected Vercel configuration: listen=%s origin=%s", cfg.Listen, cfg.Origin)
	}
	t.Setenv("PORT", "0")
	if _, err := loadConfig(); err == nil {
		t.Fatal("invalid listening port accepted")
	}
}

func TestVercelPreviewNeedsItsOwnDatabase(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("PORT", "8080")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("DATABASE_ADMIN_URL", "postgres://example:password@example.test/postgres")
	t.Setenv("APP_SECRET", "a-test-secret-with-at-least-32-characters")
	t.Setenv("ADMIN_EMAIL", "admin@example.test")
	t.Setenv("ADMIN_PASSWORD", "a-test-password-only")
	t.Setenv("APP_ORIGIN", "https://example.test")
	t.Setenv("MEZO_ALLOW_PREVIEW", "")
	for _, env := range []string{"preview", "development"} {
		t.Setenv("VERCEL_ENV", env)
		if _, err := loadConfig(); err == nil {
			t.Fatalf("%s deployment started with the shared configuration", env)
		}
	}
	t.Setenv("MEZO_ALLOW_PREVIEW", "true")
	if _, err := loadConfig(); err != nil {
		t.Fatal("explicitly allowed preview refused", err)
	}
	t.Setenv("MEZO_ALLOW_PREVIEW", "")
	for _, env := range []string{"production", ""} {
		t.Setenv("VERCEL_ENV", env)
		if _, err := loadConfig(); err != nil {
			t.Fatalf("production refused (VERCEL_ENV=%q): %v", env, err)
		}
	}
}
