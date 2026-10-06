package main

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

// Exercise the production path with CREATEDB/CREATEROLE rather than superuser.
// This opt-in test creates and removes only a randomly named role and tenant.
func TestProvisionerWithoutSuperuser(t *testing.T) {
	if os.Getenv("MEZO_INTEGRATION") != "1" {
		t.Skip("requires the opt-in PostgreSQL integration environment")
	}
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, err := pgx.Connect(ctx, cfg.AdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close(context.Background())
	var superuser bool
	if err := root.QueryRow(ctx, "SELECT rolsuper FROM pg_roles WHERE rolname=current_user").Scan(&superuser); err != nil {
		t.Fatal(err)
	}
	if !superuser {
		t.Skip("creating the temporary non-superuser provisioner requires a local superuser fixture")
	}
	role := "mezo_test_provisioner_" + randomID()
	pass := randomID() + randomID()
	ident := pgx.Identifier{role}.Sanitize()
	if _, err := root.Exec(ctx, "CREATE ROLE "+ident+" LOGIN NOSUPERUSER CREATEDB CREATEROLE PASSWORD '"+pass+"'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if _, e := root.Exec(cleanCtx, "DROP ROLE "+ident); e != nil {
			t.Error(e)
		}
	}()
	// The existing control database is owned by the app role; grant administration
	// to this temporary provisioner so it can perform the idempotent startup grant.
	if _, err := root.Exec(ctx, "GRANT mezo_control_app TO "+ident+" WITH ADMIN TRUE, INHERIT TRUE, SET TRUE"); err != nil {
		t.Fatal(err)
	}
	managedURL, err := url.Parse(cfg.AdminURL)
	if err != nil {
		t.Fatal(err)
	}
	if managedURL.Scheme != "postgres" && managedURL.Scheme != "postgresql" {
		t.Fatal("test fixture requires a PostgreSQL URL")
	}
	managedURL.User = url.UserPassword(role, pass)
	cfg.AdminURL = managedURL.String()
	a, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	var actualRole string
	if err := a.admin.QueryRow(ctx, "SELECT current_user, rolsuper FROM pg_roles WHERE rolname=current_user").Scan(&actualRole, &superuser); err != nil {
		t.Fatal(err)
	}
	if actualRole != role || superuser {
		t.Fatal("the test must run with its non-superuser provisioner")
	}
	tenant, provisionErr := a.provision(ctx, "test-managed-"+randomID(), "Teste de provisionador", "staff@example.test", "Teste", randomID(), 1)
	defer func() {
		a.close()
		cleanCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if tenant.DBName != "mezo_t_"+tenant.ID || tenant.DBRole != "mezo_u_"+tenant.ID {
			t.Fatal("unexpected test tenant identifiers")
		}
		controlCfg, e := a.connection("mezo_control", "mezo_control_app", "")
		if e != nil {
			t.Error(e)
			return
		}
		// Use the original fixture superuser for cleanup even if provisioning failed.
		controlCfg.ConnConfig.User = root.Config().User
		controlCfg.ConnConfig.Password = root.Config().Password
		control, e := pgx.ConnectConfig(cleanCtx, controlCfg.ConnConfig)
		if e != nil {
			t.Error(e)
			return
		}
		defer control.Close(context.Background())
		if _, e = control.Exec(cleanCtx, "DELETE FROM tenants WHERE id=$1", tenant.ID); e != nil {
			t.Error(e)
		}
		if _, e = root.Exec(cleanCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{tenant.DBName}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Error(e)
		}
		if _, e = root.Exec(cleanCtx, "DROP ROLE IF EXISTS "+pgx.Identifier{tenant.DBRole}.Sanitize()); e != nil {
			t.Error(e)
		}
	}()
	if provisionErr != nil {
		t.Fatal(provisionErr)
	}
	var publicConnect bool
	if err := root.QueryRow(ctx, "SELECT COALESCE(bool_or(a.grantee=0 AND a.privilege_type='CONNECT'),false) FROM pg_database d CROSS JOIN LATERAL aclexplode(d.datacl) a WHERE d.datname=$1", tenant.DBName).Scan(&publicConnect); err != nil {
		t.Fatal(err)
	}
	if publicConnect {
		t.Fatal("the provisioner left public access on the tenant database")
	}
}
