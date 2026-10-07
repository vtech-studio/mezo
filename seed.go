package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

func (a *App) seedDemo(ctx context.Context) error {
	if os.Getenv("SEED_DEMO") != "true" {
		return nil
	}
	password := os.Getenv("DEMO_STAFF_PASSWORD")
	if len(password) < 12 || len(password) > 72 {
		return errors.New("DEMO_STAFF_PASSWORD precisa de 12–72 caracteres")
	}
	_, err := a.tenant(ctx, "casa-do-sol")
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	t, err := a.provision(ctx, "casa-do-sol", "Casa do Sol", "ana@mezo.local", "Ana", password, 12)
	if err != nil {
		return err
	}
	p, release, err := a.lease(ctx, t)
	if err != nil {
		return err
	}
	defer release()
	data, err := assets.ReadFile("fixtures/demo-menu.json")
	if err != nil {
		return err
	}
	var items []struct {
		ID, Name, Description, Category, Art string
		Price                                int
		Tags                                 []string
	}
	if err = json.Unmarshal(data, &items); err != nil {
		return err
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, d := range items {
		tags, _ := json.Marshal(d.Tags)
		if _, err = tx.Exec(ctx, "INSERT INTO menu_items(id,name,description,price,category,art,tags) VALUES($1,$2,$3,$4,$5,$6,$7)", d.ID, d.Name, d.Description, d.Price, d.Category, d.Art, tags); err != nil {
			return err
		}
	}
	// Development fixture only: consumption is still read and calculated in PostgreSQL.
	if _, err = tx.Exec(ctx, "INSERT INTO table_sessions(id,table_number,code) VALUES($1,8,$2)", randomID(), newTableCode()); err != nil {
		return err
	}
	for _, line := range []struct {
		id  string
		qty int
	}{{"burrata", 1}, {"risoto", 1}, {"agua", 2}, {"limonada", 1}} {
		if _, err = tx.Exec(ctx, "INSERT INTO consumption(id,session_id,menu_id,name,price,qty,created_by) SELECT $1,s.id,m.id,m.name,m.price,$3,u.id FROM table_sessions s,menu_items m,staff u WHERE s.table_number=8 AND s.closed_at IS NULL AND m.id=$2 AND u.email='ana@mezo.local'", randomID(), line.id, line.qty); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
