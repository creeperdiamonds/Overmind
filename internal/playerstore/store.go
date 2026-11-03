// Package playerstore keeps player data, one JSON file per account, in the
// format of Dragonfly's playerdb. It implements player.Provider for both
// editions, each in its own folder.
//
// Saving takes a snapshot on the caller's goroutine (the world's) and writes it
// on the store's own goroutine: temp file, fsync, rename, so a crash leaves the
// old file or the new one, never half of one. A record that cannot be read is
// moved aside as .corrupt and the player starts fresh; nothing panics.
package playerstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// ErrNone is what Load returns for an account with no saved data.
var ErrNone = errors.New("playerstore: no saved data")

// Store is a folder of player records.
type Store struct {
	dir string
	log *slog.Logger

	mu      sync.Mutex
	pending map[uuid.UUID]playerdb.Data // written next, newest only
	writing map[uuid.UUID]playerdb.Data // being written now; Load still sees them
	wake    chan struct{}
	closed  bool
	done    chan struct{}
}

var _ player.Provider = (*Store)(nil)

// Open opens or creates the folder and starts the writer.
func Open(dir string, log *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("playerstore: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Store{dir: dir, log: log, pending: map[uuid.UUID]playerdb.Data{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go s.writer()
	return s, nil
}

func (s *Store) path(id uuid.UUID) string { return filepath.Join(s.dir, id.String()+".json") }

// Save snapshots the player now and writes it soon. The snapshot is the only
// work done on the caller's goroutine; marshalling and the disk are the
// writer's.
func (s *Store) Save(id uuid.UUID, conf player.Config, w *world.World) error {
	d := playerdb.Snapshot(conf, w)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("playerstore: closed")
	}
	s.pending[id] = d
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// Load reads an account's data. With none it returns ErrNone. A record that
// cannot be read is logged and moved aside, and an error is returned, so the
// player starts fresh and its next save does not overwrite the evidence.
func (s *Store) Load(id uuid.UUID, lookup func(world.Dimension) *world.World) (conf player.Config, w *world.World, err error) {
	defer func() {
		// Restore already turns playerdb's panics into errors; this is for
		// anything else, since Dragonfly loads Bedrock players outside any
		// recover and a panic there ends the process.
		if r := recover(); r != nil {
			conf, w, err = player.Config{}, nil, s.corrupt(id, fmt.Errorf("panic: %v", r))
		}
	}()
	s.mu.Lock()
	d, queued := s.pending[id]
	if !queued {
		d, queued = s.writing[id]
	}
	s.mu.Unlock()
	if !queued {
		b, err := os.ReadFile(s.path(id))
		if errors.Is(err, fs.ErrNotExist) {
			return player.Config{}, nil, ErrNone
		}
		if err != nil {
			return player.Config{}, nil, s.corrupt(id, err)
		}
		if err := json.Unmarshal(b, &d); err != nil {
			return player.Config{}, nil, s.corrupt(id, err)
		}
	}
	conf, w, err = playerdb.Restore(d, lookup)
	if err == nil && conf.UUID != id {
		err = fmt.Errorf("record is for %v", conf.UUID)
	}
	if err != nil {
		return player.Config{}, nil, s.corrupt(id, err)
	}
	return conf, w, nil
}

// corrupt moves a bad record aside, timestamped, and says why.
func (s *Store) corrupt(id uuid.UUID, why error) error {
	from := s.path(id)
	to := fmt.Sprintf("%s.%s.corrupt", from, time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.Rename(from, to); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.log.Error("player data unreadable and could not be moved aside", "account", id, "err", why, "move", err)
	} else {
		s.log.Error("player data unreadable; moved aside, the player starts fresh", "account", id, "err", why, "file", to)
	}
	return fmt.Errorf("playerstore: %v: %w", id, why)
}

// writer writes pending records until the store is closed and drained.
func (s *Store) writer() {
	defer close(s.done)
	for {
		s.mu.Lock()
		batch := s.pending
		s.pending = map[uuid.UUID]playerdb.Data{}
		s.writing = batch
		closed := s.closed
		s.mu.Unlock()
		for id, d := range batch {
			b, err := json.Marshal(d)
			if err == nil {
				err = writeAtomic(s.path(id), b)
			}
			if err != nil {
				s.log.Error("save player data", "account", id, "err", err)
			}
		}
		s.mu.Lock()
		s.writing = nil
		s.mu.Unlock()
		if closed && len(batch) == 0 {
			return
		}
		if len(batch) == 0 {
			<-s.wake
		}
	}
}

// writeAtomic writes b to path through a temp file in the same folder: write,
// fsync, rename.
func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// Close writes everything still pending and stops. Saves after Close fail.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		<-s.done
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	<-s.done
	return nil
}
