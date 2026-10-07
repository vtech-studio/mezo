package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	AdminURL, Secret, AdminEmail, AdminPassword, Listen, Origin string
	CloudName, CloudKey, CloudSecret                            string
	Secure                                                      bool
	Serverless                                                  bool
}

func loadConfig() (Config, error) {
	c := Config{AdminURL: os.Getenv("DATABASE_ADMIN_URL"), Secret: os.Getenv("APP_SECRET"), AdminEmail: strings.ToLower(os.Getenv("ADMIN_EMAIL")), AdminPassword: os.Getenv("ADMIN_PASSWORD"), Listen: os.Getenv("LISTEN_ADDR"), Origin: strings.TrimRight(os.Getenv("APP_ORIGIN"), "/")}
	c.CloudName, c.CloudKey, c.CloudSecret = os.Getenv("CLOUDINARY_CLOUD_NAME"), os.Getenv("CLOUDINARY_API_KEY"), os.Getenv("CLOUDINARY_API_SECRET")
	if c.CloudName != "" || c.CloudKey != "" || c.CloudSecret != "" {
		if !cloudNamePattern.MatchString(c.CloudName) || c.CloudKey == "" || c.CloudSecret == "" {
			return c, errors.New("defina as três variáveis CLOUDINARY_CLOUD_NAME, CLOUDINARY_API_KEY e CLOUDINARY_API_SECRET")
		}
	}
	if c.Listen == "" {
		if port := os.Getenv("PORT"); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return c, errors.New("PORT inválida")
			}
			c.Listen = ":" + port
		} else {
			c.Listen = "127.0.0.1:4173"
		}
	}
	c.Serverless = os.Getenv("VERCEL") == "1"
	// Vercel previews inherit any variable shared with production. Refuse to
	// start them unless a separate database was configured on purpose.
	if env := os.Getenv("VERCEL_ENV"); c.Serverless && env != "" && env != "production" && os.Getenv("MEZO_ALLOW_PREVIEW") != "true" {
		return c, errors.New("deploy de prévia desativado: configure um PostgreSQL separado e MEZO_ALLOW_PREVIEW=true no ambiente Preview")
	}
	if c.Origin == "" {
		if c.Serverless {
			return c, errors.New("defina APP_ORIGIN com a URL HTTPS de produção na Vercel")
		}
		c.Origin = "http://127.0.0.1:4173"
	}
	c.Secure = strings.HasPrefix(c.Origin, "https://")
	if c.Serverless && !c.Secure {
		return c, errors.New("APP_ORIGIN deve usar HTTPS na Vercel")
	}
	if c.AdminURL == "" || len(c.Secret) < 32 || c.AdminEmail == "" || len(c.AdminPassword) < 12 || len(c.AdminPassword) > 72 {
		return c, errors.New("defina DATABASE_ADMIN_URL, APP_SECRET (32+ caracteres), ADMIN_EMAIL e ADMIN_PASSWORD (12–72 bytes)")
	}
	return c, nil
}

type Tenant struct {
	ID, Slug, Name, DBName, DBRole, DBSecret, Status string
	Revision                                         int64
}
type cachedPool struct {
	pool  *pgxpool.Pool
	refs  int
	used  time.Time
	ready chan struct{} // closed once pool or err is set
	err   error
}
type App struct {
	cfg            Config
	admin, control *pgxpool.Pool
	adminRole      string
	mu             sync.Mutex
	pools          map[string]*cachedPool
	hub            *Hub
	openPool       func(context.Context, Tenant) (*pgxpool.Pool, error) // tests replace the restaurant connection
	imageClient    *http.Client                                         // tests replace only the Cloudinary HTTP transport
}

func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (a *App) crypt(value string, encrypt bool) (string, error) {
	key := sha256.Sum256([]byte(a.cfg.Secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if encrypt {
		nonce := make([]byte, gcm.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return "", err
		}
		return base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(value), []byte("mezo/tenant-password/v1"))), nil
	}
	b, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(b) < gcm.NonceSize() {
		return "", errors.New("credencial de tenant inválida")
	}
	plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], []byte("mezo/tenant-password/v1"))
	return string(plain), err
}
func databasePoolConfig(c Config) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(c.AdminURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 3
	if c.Serverless {
		cfg.MaxConns = 1
	}
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 2 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	if strings.HasSuffix(strings.ToLower(cfg.ConnConfig.Host), ".pooler.supabase.com") {
		if cfg.ConnConfig.Port != 5432 {
			return nil, errors.New("use o Session pooler do Supabase na porta 5432; o provisionamento exige locks de sessão")
		}
		if cfg.ConnConfig.TLSConfig == nil {
			return nil, errors.New("a conexão com o Supabase exige TLS: use sslmode=require ou verify-full")
		}
		for _, fallback := range cfg.ConnConfig.Fallbacks {
			if fallback.TLSConfig == nil {
				return nil, errors.New("a conexão com o Supabase exige TLS: use sslmode=require ou verify-full")
			}
		}
		role, project, ok := strings.Cut(cfg.ConnConfig.User, ".")
		if !ok || role == "" || project == "" {
			return nil, errors.New("o usuário do pooler Supabase deve incluir o identificador do projeto")
		}
	}
	return cfg, nil
}
func (a *App) connection(db, user, password string) (*pgxpool.Config, error) {
	cfg, err := databasePoolConfig(a.cfg)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(strings.ToLower(cfg.ConnConfig.Host), ".pooler.supabase.com") {
		_, project, _ := strings.Cut(cfg.ConnConfig.User, ".")
		user += "." + project
	}
	cfg.ConnConfig.Database = db
	cfg.ConnConfig.User = user
	cfg.ConnConfig.Password = password
	return cfg, nil
}
func migrate(ctx context.Context, cfg *pgx.ConnConfig, kind string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(1)
	defer db.Close()
	sub, err := fs.Sub(assets, "migrations/"+kind)
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 25), lock.WithUnlockTimeout(1, 3))
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	// Most starts find nothing to apply. Checking does not take the lock, whose
	// retry interval would otherwise delay concurrent cold starts by a second.
	if pending, err := p.HasPending(ctx); err == nil && !pending {
		return nil
	}
	_, err = p.Up(ctx)
	return err
}
func newApp(ctx context.Context, cfg Config) (*App, error) {
	adminCfg, err := databasePoolConfig(cfg)
	if err != nil {
		return nil, err
	}
	adminCfg.MaxConns = 2
	if cfg.Serverless {
		adminCfg.MaxConns = 1
	}
	admin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, admin: admin, pools: make(map[string]*cachedPool), hub: newHub()}
	failed := true
	defer func() {
		if failed {
			a.close()
		}
	}()
	lock, err := admin.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	// Poolers can encode routing information in the login name. PostgreSQL's
	// current_user is the actual role that must receive database ownership grants.
	if err = lock.QueryRow(ctx, "SELECT current_user").Scan(&a.adminRole); err != nil {
		return nil, err
	}
	passHash := sha256.Sum256([]byte(cfg.Secret + "/mezo-control"))
	password := hex.EncodeToString(passHash[:])
	// After the first start the central role, database and grants already exist:
	// skip the cross-instance lock and the DDL on every cold start.
	var ready bool
	if err = lock.QueryRow(ctx, `SELECT CASE WHEN EXISTS(SELECT 1 FROM pg_database WHERE datname='mezo_control') THEN
 NOT has_database_privilege('public','mezo_control','CONNECT') AND EXISTS(SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid
 WHERE r.rolname='mezo_control_app' AND m.member=(SELECT oid FROM pg_roles WHERE rolname=current_user) AND m.inherit_option AND m.set_option)
 ELSE false END`).Scan(&ready); err != nil {
		return nil, err
	}
	if !ready {
		if err = a.prepareControl(ctx, lock, password); err != nil {
			return nil, err
		}
	}
	poolCfg, err := a.connection("mezo_control", "mezo_control_app", password)
	if err != nil {
		return nil, err
	}
	if err = migrate(ctx, poolCfg.ConnConfig, "control"); err != nil {
		return nil, err
	}
	a.control, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	// The initial account is created once; hashing on every cold start only costs time.
	var adminExists bool
	if err = a.control.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM administrators WHERE email=$1)", cfg.AdminEmail).Scan(&adminExists); err != nil {
		return nil, err
	}
	if !adminExists {
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if _, err = a.control.Exec(ctx, "INSERT INTO administrators(email,password_hash) VALUES($1,$2) ON CONFLICT(email) DO NOTHING", cfg.AdminEmail, string(hash)); err != nil {
			return nil, err
		}
	}
	_, _ = a.control.Exec(ctx, "DELETE FROM auth_sessions WHERE expires_at<now()")
	_, _ = a.control.Exec(ctx, "DELETE FROM login_attempts WHERE reset_at<now()")
	failed = false
	return a, nil
}

// prepareControl creates the central role and database, serialized across instances.
func (a *App) prepareControl(ctx context.Context, conn *pgxpool.Conn, password string) error {
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(74829101)"); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(74829101)")
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mezo_control_app')").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE ROLE mezo_control_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD '"+password+"'"); err != nil {
			return err
		}
	}
	// PostgreSQL 16+ requires SET membership to assign database ownership. INHERIT
	// also lets a CREATEDB/CREATEROLE provisioner revoke PUBLIC database access.
	if _, err := conn.Exec(ctx, "GRANT mezo_control_app TO "+pgx.Identifier{a.adminRole}.Sanitize()+" WITH INHERIT TRUE, SET TRUE"); err != nil {
		return err
	}
	if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='mezo_control')").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE DATABASE mezo_control OWNER mezo_control_app"); err != nil {
			return err
		}
	}
	_, err := conn.Exec(ctx, "REVOKE CONNECT ON DATABASE mezo_control FROM PUBLIC")
	return err
}
func (a *App) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pools {
		if p.pool != nil {
			p.pool.Close()
		}
	}
	if a.control != nil {
		a.control.Close()
	}
	if a.admin != nil {
		a.admin.Close()
	}
}
func (a *App) tenant(ctx context.Context, slug string) (Tenant, error) {
	var t Tenant
	err := a.control.QueryRow(ctx, "SELECT id,slug,name,db_name,db_role,db_secret,status,revision FROM tenants WHERE slug=$1 AND status='active'", slug).Scan(&t.ID, &t.Slug, &t.Name, &t.DBName, &t.DBRole, &t.DBSecret, &t.Status, &t.Revision)
	return t, err
}
func (a *App) listTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := a.control.Query(ctx, "SELECT id,slug,name,db_name,db_role,db_secret,status FROM tenants ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tenant{}
	for rows.Next() {
		var t Tenant
		if err = rows.Scan(&t.ID, &t.Slug, &t.Name, &t.DBName, &t.DBRole, &t.DBSecret, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (a *App) lease(ctx context.Context, t Tenant) (*pgxpool.Pool, func(), error) {
	a.mu.Lock()
	entry := a.pools[t.ID]
	var evicted *pgxpool.Pool
	opening := entry == nil
	if opening {
		if len(a.pools) >= 16 {
			var oldestID string
			var oldest time.Time
			for id, p := range a.pools {
				if p.refs == 0 && (oldestID == "" || p.used.Before(oldest)) {
					oldestID = id
					oldest = p.used
				}
			}
			if oldestID == "" {
				a.mu.Unlock()
				return nil, nil, errors.New("capacidade de conexões atingida")
			}
			evicted = a.pools[oldestID].pool
			delete(a.pools, oldestID)
		}
		entry = &cachedPool{ready: make(chan struct{})}
		a.pools[t.ID] = entry
	}
	entry.refs++
	a.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { a.mu.Lock(); entry.refs--; entry.used = time.Now(); a.mu.Unlock() }) }
	if evicted != nil {
		go evicted.Close()
	}
	if opening {
		// Connect and migrate outside the global lock, so a slow restaurant
		// database never holds requests for the others. Concurrent requests
		// for the same restaurant wait for this single attempt.
		open := a.openPool
		if open == nil {
			open = a.openTenant
		}
		pool, err := open(ctx, t)
		a.mu.Lock()
		entry.pool, entry.err = pool, err
		if err != nil && a.pools[t.ID] == entry {
			delete(a.pools, t.ID) // the next access tries again
		}
		a.mu.Unlock()
		close(entry.ready)
	}
	select {
	case <-entry.ready:
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	}
	if entry.err != nil {
		release()
		return nil, nil, entry.err
	}
	return entry.pool, release, nil
}

// openTenant connects to one restaurant database after applying its pending
// migrations. It outlives the request that triggered it, so a client giving up
// does not fail the other requests waiting for the same restaurant.
func (a *App) openTenant(ctx context.Context, t Tenant) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	password, err := a.crypt(t.DBSecret, false)
	if err != nil {
		return nil, err
	}
	cfg, err := a.connection(t.DBName, t.DBRole, password)
	if err != nil {
		return nil, err
	}
	// Migrate only the restaurant being accessed; coordinate concurrent instances.
	if err = migrate(ctx, cfg.ConnConfig, "tenant"); err != nil {
		return nil, fmt.Errorf("migração de %s: %w", t.Slug, err)
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
func (a *App) provision(ctx context.Context, slug, name, email, staffName, password string, tables int) (Tenant, error) {
	t := Tenant{ID: randomID(), Slug: slug, Name: name, Status: "provisioning"}
	t.DBName = "mezo_t_" + t.ID
	t.DBRole = "mezo_u_" + t.ID
	dbPassword := randomID() + randomID()
	var err error
	t.DBSecret, err = a.crypt(dbPassword, true)
	if err != nil {
		return t, err
	}
	if _, err = a.control.Exec(ctx, "INSERT INTO tenants(id,slug,name,db_name,db_role,db_secret,status) VALUES($1,$2,$3,$4,$5,$6,$7)", t.ID, t.Slug, t.Name, t.DBName, t.DBRole, t.DBSecret, t.Status); err != nil {
		return t, err
	}
	defer func() {
		if err != nil {
			_, _ = a.control.Exec(context.Background(), "UPDATE tenants SET status='failed' WHERE id=$1", t.ID)
		}
	}()
	// Identifiers are server-generated; the password is generated hexadecimal, never user SQL.
	if _, err = a.admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{t.DBRole}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD '"+dbPassword+"'"); err != nil {
		return t, err
	}
	if _, err = a.admin.Exec(ctx, "GRANT "+pgx.Identifier{t.DBRole}.Sanitize()+" TO "+pgx.Identifier{a.adminRole}.Sanitize()+" WITH INHERIT TRUE, SET TRUE"); err != nil {
		return t, err
	}
	if _, err = a.admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{t.DBName}.Sanitize()+" OWNER "+pgx.Identifier{t.DBRole}.Sanitize()); err != nil {
		return t, err
	}
	if _, err = a.admin.Exec(ctx, "REVOKE CONNECT ON DATABASE "+pgx.Identifier{t.DBName}.Sanitize()+" FROM PUBLIC"); err != nil {
		return t, err
	}
	pc, err := a.connection(t.DBName, t.DBRole, dbPassword)
	if err != nil {
		return t, err
	}
	if err = migrate(ctx, pc.ConnConfig, "tenant"); err != nil {
		return t, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return t, err
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return t, err
	}
	defer tx.Rollback(ctx)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return t, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO staff(id,name,email,password_hash,role) VALUES($1,$2,$3,$4,'manager')", randomID(), staffName, strings.ToLower(email), string(hash)); err != nil {
		return t, err
	}
	// Tables start closed: a printed QR only reaches a bill after the team opens the table.
	for n := 1; n <= tables; n++ {
		if _, err = tx.Exec(ctx, "INSERT INTO dining_tables(number,qr_token) VALUES($1,$2)", n, randomID()); err != nil {
			return t, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return t, err
	}
	_, err = a.control.Exec(ctx, "UPDATE tenants SET status='active' WHERE id=$1", t.ID)
	t.Status = "active"
	return t, err
}
