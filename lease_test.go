package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLeaseOpensRestaurantsOutsideTheGlobalLock(t *testing.T) {
	slow := make(chan struct{})
	var mu sync.Mutex
	opened := map[string]int{}
	count := func(id string) int { mu.Lock(); defer mu.Unlock(); return opened[id] }
	a := &App{pools: map[string]*cachedPool{}}
	a.openPool = func(_ context.Context, tenant Tenant) (*pgxpool.Pool, error) {
		mu.Lock()
		opened[tenant.ID]++
		mu.Unlock()
		switch tenant.ID {
		case "slow":
			<-slow
		case "broken":
			return nil, errors.New("banco indisponível")
		}
		// Pools connect lazily: nothing listens on this address.
		return pgxpool.New(context.Background(), "postgres://mezo:mezo@127.0.0.1:1/mezo")
	}
	defer a.close()
	ctx := context.Background()
	slowDone := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, release, err := a.lease(ctx, Tenant{ID: "slow"})
			if err == nil {
				release()
			}
			slowDone <- err
		}()
	}
	for count("slow") == 0 {
		time.Sleep(time.Millisecond)
	}
	fast := make(chan error, 1)
	go func() {
		pool, release, err := a.lease(ctx, Tenant{ID: "fast"})
		if err == nil {
			if pool == nil {
				err = errors.New("pool ausente")
			}
			release()
		}
		fast <- err
	}()
	select {
	case err := <-fast:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("um restaurante lento bloqueou o acesso a outro restaurante")
	}
	// A request that gives up stops waiting without keeping a reference.
	waiting, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := a.lease(waiting, Tenant{ID: "slow"}); !errors.Is(err, context.Canceled) {
		t.Fatal("pedido cancelado continuou esperando", err)
	}
	close(slow)
	for i := 0; i < 2; i++ {
		if err := <-slowDone; err != nil {
			t.Fatal(err)
		}
	}
	if count("slow") != 1 {
		t.Fatalf("acessos simultâneos abriram o restaurante %d vezes", count("slow"))
	}
	a.mu.Lock()
	refs := a.pools["slow"].refs
	a.mu.Unlock()
	if refs != 0 {
		t.Fatalf("referências pendentes: %d", refs)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := a.lease(ctx, Tenant{ID: "broken"}); err == nil {
			t.Fatal("falha de conexão ignorada")
		}
	}
	if count("broken") != 2 {
		t.Fatal("a falha ficou em cache em vez de tentar novamente")
	}
}
