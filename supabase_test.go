package main

import (
	"strings"
	"testing"
)

func TestSupabaseTenantConnectionKeepsProjectRouting(t *testing.T) {
	a := &App{cfg: Config{
		AdminURL:   "postgres://postgres.exampleproject:admin-password@aws-0-sa-east-1.pooler.supabase.com:5432/postgres?sslmode=require",
		Serverless: true,
	}}
	for _, db := range []string{"mezo_control", "mezo_t_test"} {
		cfg, err := a.connection(db, "tenant_role", "tenant-password")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ConnConfig.User != "tenant_role.exampleproject" || cfg.ConnConfig.Database != db || cfg.ConnConfig.Password != "tenant-password" {
			t.Fatal("the tenant connection lost its own database, role, password or project routing")
		}
		if cfg.MaxConns != 1 || cfg.ConnConfig.TLSConfig == nil {
			t.Fatal("serverless Supabase connections need a small pool and TLS")
		}
	}
}

func TestSupabaseProvisioningRejectsIncompatiblePooler(t *testing.T) {
	for _, test := range []struct {
		name, url, message string
	}{
		{"transaction mode", "postgres://postgres.ref:password@aws-0-sa-east-1.pooler.supabase.com:6543/postgres?sslmode=require", "Session pooler"},
		{"unencrypted", "postgres://postgres.ref:password@aws-0-sa-east-1.pooler.supabase.com:5432/postgres?sslmode=disable", "TLS"},
		{"plaintext fallback", "postgres://postgres.ref:password@aws-0-sa-east-1.pooler.supabase.com:5432/postgres?sslmode=prefer", "TLS"},
		{"no project", "postgres://postgres:password@aws-0-sa-east-1.pooler.supabase.com:5432/postgres?sslmode=require", "identificador"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := databasePoolConfig(Config{AdminURL: test.url})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %s configuration error, got %v", test.message, err)
			}
		})
	}
}

func TestOrdinaryPostgresRolesAreNotRewritten(t *testing.T) {
	a := &App{cfg: Config{AdminURL: "postgres://postgres:password@127.0.0.1:5432/postgres?sslmode=disable"}}
	cfg, err := a.connection("tenant_db", "role.with.dot", "tenant-password")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.User != "role.with.dot" || cfg.MaxConns != 3 {
		t.Fatal("Supabase routing must not affect ordinary PostgreSQL connections")
	}
}
