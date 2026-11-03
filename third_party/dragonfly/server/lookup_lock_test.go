package server

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Overmind fork: PlayerByName and PlayerByXUID take the player lock, as the
// other lookups do. Run with -race: a join or leave writing the player map
// while a lookup reads it is a data race without the lock.
func TestPlayerLookupsTakeTheLock(t *testing.T) {
	var conf Config
	srv := conf.New()
	defer func() { _ = srv.World().Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			id := uuid.New()
			srv.pmu.Lock()
			srv.p[id] = &onlinePlayer{name: "someone", xuid: "1"}
			srv.pmu.Unlock()
			srv.pmu.Lock()
			delete(srv.p, id)
			srv.pmu.Unlock()
		}
	}()
	for i := 0; i < 500; i++ {
		srv.PlayerByName("nobody")
		srv.PlayerByXUID("0")
	}
	wg.Wait()
}
