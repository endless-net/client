package connectivity

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	signing "github.com/endless-net/signing/api/contract"
)

// These test-only ports exercise the real protected Store/CAS. Windows DPAPI,
// ACL and atomic file replacement have separate native owner fixtures.
type cursorBackend struct {
	mu             sync.Mutex
	data           []byte
	loseACK        bool
	afterExclusive func()
}

func (b *cursorBackend) Exclusive(ctx context.Context, fn func(state.Slot) error) error {
	b.mu.Lock()
	if ctx.Err() != nil {
		b.mu.Unlock()
		return ctx.Err()
	}
	err := fn(b)
	after := b.afterExclusive
	b.afterExclusive = nil
	b.mu.Unlock()
	if after != nil {
		after()
	}
	return err
}
func (b *cursorBackend) Read(limit int64) ([]byte, error) {
	if b.data == nil {
		return nil, state.ErrMissing
	}
	if int64(len(b.data)) > limit {
		return nil, state.ErrCorrupt
	}
	return append([]byte(nil), b.data...), nil
}
func (b *cursorBackend) Replace(raw []byte) error {
	b.data = append([]byte(nil), raw...)
	if b.loseACK {
		return state.ErrUncertain
	}
	return nil
}

type cursorCodec struct{ cipher.AEAD }

func (c cursorCodec) Seal(raw []byte) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(nonce, nonce, raw, nil), nil
}
func (c cursorCodec) Open(raw []byte) ([]byte, error) {
	if len(raw) < c.NonceSize() {
		return nil, state.ErrCorrupt
	}
	return c.AEAD.Open(nil, raw[:c.NonceSize()], raw[c.NonceSize():], nil)
}

func cursorStore(t *testing.T, f *mapFixture) (*state.Store, *cursorBackend) {
	t.Helper()
	b := &cursorBackend{}
	block, err := aes.NewCipher(bytes.Repeat([]byte{65}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.New(b, cursorCodec{aead}, "753c9536-feb4-43a5-8d55-7a3c1b7e215d", 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(context.Background(), 1, func(value *state.State) error {
		value.OwnerSID = f.profile.Attempt.Owner
		value.ActiveProfileID = f.profile.ID
		value.Profiles[f.profile.ID] = f.profile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, b
}

func TestPersistedMapCursorSurvivesLostACKAndCannotBecomeAccessOnRestart(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	m, err := v.Verify(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, b := cursorStore(t, f)
	b.loseACK = true
	if err := PersistCursor(context.Background(), s, 1, m); !errors.Is(err, state.ErrUncertain) {
		t.Fatal("uncertain cursor commit reported success", err)
	}
	b.loseACK = false
	restarted, err := state.New(b, safeCodec(t), "753c9536-feb4-43a5-8d55-7a3c1b7e215d", 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := stored.Profiles[f.profile.ID]
	if p.Connection.MapCursor == nil || p.Connection.MapCursor.Revision.Network != 9007199254740993 || p.Connection.Journal != nil {
		t.Fatal("cursor lost precision or implied native apply")
	}
	if _, err := v.Verify(context.Background(), raw, p.Connection.MapCursor); !errors.Is(err, ErrReplay) {
		t.Fatal("restart replay became fresh authority", err)
	}
	for _, change := range []func(*state.MapCursor){
		func(c *state.MapCursor) { c.Revision.Network-- },
		func(c *state.MapCursor) { c.IssuedAt = c.IssuedAt.Add(-time.Second) },
		func(c *state.MapCursor) { c.PayloadHash = "" },
		func(c *state.MapCursor) { c.Revision.Global = 1 },
	} {
		current, err := s.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = s.Update(context.Background(), current.Revision, func(value *state.State) error {
			p := value.Profiles[f.profile.ID]
			change(p.Connection.MapCursor)
			value.Profiles[p.ID] = p
			return nil
		})
		if !errors.Is(err, state.ErrCorrupt) {
			t.Fatal("stored high-water mark rewound/corrupted", err)
		}
	}
}

func safeCodec(t *testing.T) cursorCodec {
	t.Helper()
	block, err := aes.NewCipher(bytes.Repeat([]byte{65}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return cursorCodec{aead}
}

func TestMapPersistenceFencesDisconnectAndConcurrentIntent(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-load", true: "between-load-and-CAS"}[concurrent], func(t *testing.T) {
			f := newMapFixture(t)
			v, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			m, err := v.Verify(context.Background(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
			if err != nil {
				t.Fatal(err)
			}
			s, b := cursorStore(t, f)
			disconnect := func() {
				current, err := s.Load(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Update(context.Background(), current.Revision, func(value *state.State) error {
					p := value.Profiles[f.profile.ID]
					_, err := p.Connection.AcceptIntent(state.IntentDisconnected, f.now.Add(time.Second))
					value.Profiles[p.ID] = p
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			if concurrent {
				b.afterExclusive = disconnect
			} else {
				disconnect()
			}
			if err := PersistCursor(context.Background(), s, 1, m); !errors.Is(err, state.ErrStale) {
				t.Fatal("late map persisted across Disconnect", err)
			}
			current, err := s.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			c := current.Profiles[f.profile.ID].Connection
			if c.Intent != state.IntentDisconnected || c.Generation != 2 || c.MapCursor != nil {
				t.Fatal("late result changed latest intent/cursor")
			}
			if err := PersistCursor(context.Background(), s, 2, m); !errors.Is(err, ErrConfig) {
				t.Fatal("old verified map was rebound to another generation", err)
			}
		})
	}
}

func TestMapPersistenceRejectsChangedTrustAndRetainsCursorOnDisconnect(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	m, err := v.Verify(context.Background(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := cursorStore(t, f)
	if err := PersistCursor(context.Background(), s, 1, m); err != nil {
		t.Fatal(err)
	}
	value, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var trust signing.ServerKeyResponse
	if err := json.Unmarshal(f.trust.Bundle, &trust); err != nil {
		t.Fatal(err)
	}
	trust.TrustBundle.Keys[0].ProviderVersion++
	raw, err := json.Marshal(trust)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := control.DecodeAnnouncement(f.trust.Origin, raw, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(context.Background(), value.Revision, func(value *state.State) error {
		p := value.Profiles[f.profile.ID]
		p.Trust = state.ServerTrust{KeyID: changed.KeyID, AnnouncementID: changed.ID, Bundle: changed.Bundle}
		value.Profiles[p.ID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := PersistCursor(context.Background(), s, 1, m); !errors.Is(err, state.ErrStale) {
		t.Fatal("in-flight map crossed trust action", err)
	}
	value, err = s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(context.Background(), value.Revision, func(value *state.State) error {
		p := value.Profiles[f.profile.ID]
		_, err := p.Connection.AcceptIntent(state.IntentDisconnected, f.now.Add(time.Second))
		value.Profiles[p.ID] = p
		return err
	}); err != nil {
		t.Fatal(err)
	}
	value, err = s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value.Profiles[f.profile.ID].Connection.MapCursor.PayloadHash != m.Cursor().PayloadHash {
		t.Fatal("Disconnect reset high-water mark")
	}
	if err := s.Update(context.Background(), value.Revision, func(value *state.State) error {
		p := value.Profiles[f.profile.ID]
		p.Connection.MapCursor.Revision.Network++
		value.Profiles[p.ID] = p
		return nil
	}); !errors.Is(err, state.ErrCorrupt) {
		t.Fatal("late disconnected cursor write accepted", err)
	}
}
