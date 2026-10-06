// Backup every Mezô database separately, without putting credentials in command arguments.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

type entry struct {
	Database string `json:"database"`
	File     string `json:"file"`
	SHA256   string `json:"sha256"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Backup incompleto:", err)
		os.Exit(1)
	}
}
func pgEnvironment(raw, database string) ([]string, error) {
	u, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_ADMIN_URL inválida")
	}
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(v), "PG") {
			env = append(env, v)
		}
	}
	sslmode := "disable"
	if u.TLSConfig != nil {
		sslmode = "require"
		if !u.TLSConfig.InsecureSkipVerify {
			sslmode = "verify-full"
		}
	}
	if parsed, e := url.Parse(raw); e == nil {
		q := parsed.Query()
		if mode := q.Get("sslmode"); mode != "" {
			sslmode = mode
		}
		for _, key := range []string{"sslrootcert", "sslcert", "sslkey"} {
			if value := q.Get(key); value != "" {
				env = append(env, "PG"+strings.ToUpper(key)+"="+value)
			}
		}
	}
	return append(env, "PGHOST="+u.Host, "PGPORT="+strconv.Itoa(int(u.Port)), "PGUSER="+u.User, "PGPASSWORD="+u.Password, "PGDATABASE="+database, "PGSSLMODE="+sslmode, "PGCONNECT_TIMEOUT=15"), nil
}
func run() error {
	envFile := flag.String("env", ".env", "arquivo local de configuração")
	out := flag.String("out", "backups", "diretório privado dos backups")
	bin := flag.String("postgres-bin", "", "pasta de pg_dump/pg_restore (PostgreSQL 17+)")
	verify := flag.Bool("verify-local", false, "restaurar cada arquivo em banco descartável; somente loopback")
	flag.Parse()
	if err := godotenv.Load(*envFile); err != nil {
		return fmt.Errorf("não foi possível ler o arquivo de configuração")
	}
	raw := os.Getenv("DATABASE_ADMIN_URL")
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		return fmt.Errorf("DATABASE_ADMIN_URL inválida")
	}
	if *verify && cfg.Host != "127.0.0.1" && cfg.Host != "localhost" && cfg.Host != "::1" {
		return fmt.Errorf("a verificação de restauração aceita somente PostgreSQL local")
	}
	if strings.HasSuffix(cfg.Host, ".pooler.supabase.com") && cfg.Port != 5432 {
		return fmt.Errorf("use o Session pooler 5432")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cfg.Database = "mezo_control"
	control, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("não foi possível conectar ao catálogo")
	}
	defer control.Close(ctx)
	rows, err := control.Query(ctx, "SELECT db_name FROM tenants WHERE status='active' ORDER BY id")
	if err != nil {
		return err
	}
	names := []string{"mezo_control"}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if !strings.HasPrefix(name, "mezo_t_") || len(name) != 39 {
			rows.Close()
			return fmt.Errorf("catálogo contém nome de banco inesperado")
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	folder := filepath.Join(*out, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err = os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	command := func(name string, args []string, env []string) error {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if *bin != "" {
			name = filepath.Join(*bin, name)
		}
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s falhou: %s", filepath.Base(name), strings.TrimSpace(string(output)))
		}
		return nil
	}
	entries := []entry{}
	for _, name := range names {
		env, err := pgEnvironment(raw, name)
		if err != nil {
			return err
		}
		file := name + ".dump"
		path := filepath.Join(folder, file)
		if err = command("pg_dump", []string{"--format=custom", "--no-owner", "--no-privileges", "--file=" + path}, env); err != nil {
			return err
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(bytes)
		entries = append(entries, entry{name, file, hex.EncodeToString(sum[:])})
		if *verify {
			if err = verifyRestore(ctx, control, cfg, raw, path, name, command); err != nil {
				return err
			}
		}
	}
	manifest, _ := json.MarshalIndent(map[string]any{"createdAt": time.Now().UTC(), "databases": entries, "restoreVerifiedLocally": *verify, "recoveryRequires": "APP_SECRET original e provisionamento dos usuários/bancos; preservar catálogo e todos os arquivos do mesmo conjunto"}, "", "  ")
	if err = os.WriteFile(filepath.Join(folder, "manifest.json"), manifest, 0600); err != nil {
		return err
	}
	absolute, _ := filepath.Abs(folder)
	fmt.Printf("Backup concluído: %d bancos. Manifesto: %s\n", len(entries), filepath.Join(absolute, "manifest.json"))
	return nil
}
func verifyRestore(ctx context.Context, admin *pgx.Conn, cfg *pgx.ConnConfig, raw, path, source string, command func(string, []string, []string) error) error {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	name := "mezo_verify_" + hex.EncodeToString(token)
	// Name is generated here and cleanup is installed ONLY after successful creation.
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = admin.Exec(clean, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	}()
	env, err := pgEnvironment(raw, name)
	if err != nil {
		return err
	}
	if err = command("pg_restore", []string{"--exit-on-error", "--no-owner", "--no-privileges", "--dbname=" + name, path}, env); err != nil {
		return err
	}
	restoredCfg := cfg.Copy()
	restoredCfg.Database = name
	restored, err := pgx.ConnectConfig(ctx, restoredCfg)
	if err != nil {
		return err
	}
	defer restored.Close(ctx)
	sourceCfg := cfg.Copy()
	sourceCfg.Database = source
	original, err := pgx.ConnectConfig(ctx, sourceCfg)
	if err != nil {
		return err
	}
	defer original.Close(ctx)
	tables := []string{"tenants", "administrators"}
	if source != "mezo_control" {
		tables = []string{"staff", "dining_tables", "menu_items", "table_sessions", "orders", "order_items", "order_events", "consumption", "calls", "settlements"}
	}
	for _, table := range tables {
		var before, after int64
		var beforeHash, afterHash string
		sql := "SELECT count(*),md5(COALESCE(string_agg(to_jsonb(t)::text,'' ORDER BY to_jsonb(t)::text),'')) FROM " + pgx.Identifier{table}.Sanitize() + " t"
		if err = original.QueryRow(ctx, sql).Scan(&before, &beforeHash); err != nil {
			return err
		}
		if err = restored.QueryRow(ctx, sql).Scan(&after, &afterHash); err != nil {
			return err
		}
		if before != after || beforeHash != afterHash {
			return fmt.Errorf("verificação de %s divergiu; execute com aplicação parada", table)
		}
	}
	fmt.Printf("Restauração em banco temporário validada: %s\n", source)
	return nil
}
