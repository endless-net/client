//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/endless-net/client/internal/client"
	"github.com/godbus/dbus/v5"
)

const (
	loginName      = "org.freedesktop.login1"
	loginPath      = dbus.ObjectPath("/org/freedesktop/login1")
	loginInterface = "org.freedesktop.login1.Manager"
	busInterface   = "org.freedesktop.DBus"
)

// The source owns a private system-bus connection and its delay FD. A failed
// subscription closes the runtime gate before retrying. Failure to recover
// terminates the agent so its service manager can restart it.
func runAgentPlatformLifecycle(ctx context.Context, run func(context.Context, <-chan client.RuntimeLifecycleNotification) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	initial, err := openLogindSession(ctx)
	if err != nil {
		return fmt.Errorf("initialize logind lifecycle source: %w", err)
	}
	events := make(chan client.RuntimeLifecycleNotification, 64)
	sourceResult := make(chan error, 1)
	go func() {
		sourceResult <- runLogindSource(ctx, events, initial)
		cancel()
	}()
	runErr := run(ctx, events)
	cancel()
	sourceErr := <-sourceResult
	if ctx.Err() != nil && sourceErr == nil {
		return runErr
	}
	return errors.Join(runErr, sourceErr)
}

func runLogindSource(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, initial *logindSession) error {
	return runLogindSourceWith(ctx, events, initial, func(ctx context.Context) (logindLifecycleSession, error) {
		return openLogindSession(ctx)
	})
}

type logindLifecycleSession interface {
	preparing(context.Context) (bool, error)
	suspend(context.Context, chan<- client.RuntimeLifecycleNotification) error
	resume(context.Context, chan<- client.RuntimeLifecycleNotification, client.RuntimeLifecycleEvent) error
	listen(context.Context, chan<- client.RuntimeLifecycleNotification) error
	isSleeping() bool
	hasResumed() bool
	close()
	suspendFailed() bool
	markSleeping()
	sessionSnapshot() map[string]logindSessionIdentity
}

func runLogindSourceWith(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, initial logindLifecycleSession, open func(context.Context) (logindLifecycleSession, error)) error {
	var recovering bool
	var missedResume bool
	var known map[string]logindSessionIdentity
	for attempt := 0; ctx.Err() == nil; attempt++ {
		session := initial
		initial = nil
		if attempt > 0 {
			// Lost signal delivery means a sleep edge may have been missed.
			// Hold the runtime gate until a fresh owner and sleep state agree.
			if !recovering {
				transition, cancel := context.WithTimeout(ctx, 30*time.Second)
				err := sendLogindEvent(transition, events, client.RuntimeSourceLost, time.Time{})
				cancel()
				if err != nil {
					return fmt.Errorf("close runtime after logind loss: %w", err)
				}
				recovering = true
			}
			if attempt > 5 {
				return errors.New("logind lifecycle source recovery exhausted")
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		if session == nil {
			var err error
			session, err = open(ctx)
			if err != nil {
				continue
			}
		}
		current := session.sessionSnapshot()
		if attempt > 0 {
			if err := deliverMissedLogoffs(ctx, events, known, current); err != nil {
				session.close()
				return err
			}
		}
		known = current
		preparing, err := session.preparing(ctx)
		if err == nil && recovering && !preparing {
			resumeEvent := client.RuntimeSourceRecovered
			if missedResume {
				resumeEvent = client.RuntimeResume
			}
			err = session.resume(ctx, events, resumeEvent)
			if err == nil {
				recovering = false
				missedResume = false
			}
		}
		if err == nil && preparing {
			err = session.suspend(ctx, events)
			if err == nil {
				session.markSleeping()
			}
		}
		if err == nil {
			err = session.listen(ctx, events)
		}
		if session.hasResumed() {
			missedResume = false
		} else if session.isSleeping() {
			missedResume = true
		}
		known = session.sessionSnapshot()
		session.close()
		if ctx.Err() != nil {
			return nil
		}
		if session.suspendFailed() {
			return errors.New("logind sleep transition was not confirmed")
		}
		_ = err // Source errors are intentionally redacted at the agent boundary.
	}
	return nil
}

func (s *logindSession) suspendFailed() bool { return s.failedSuspend }
func (s *logindSession) markSleeping()       { s.sleeping, s.resumed = true, false }
func (s *logindSession) isSleeping() bool    { return s.sleeping }
func (s *logindSession) hasResumed() bool    { return s.resumed }
func (s *logindSession) sessionSnapshot() map[string]logindSessionIdentity {
	return cloneLogindSessions(s.sessions)
}

type logindSessionIdentity struct {
	uid  uint32
	path dbus.ObjectPath
}

func cloneLogindSessions(source map[string]logindSessionIdentity) map[string]logindSessionIdentity {
	copy := make(map[string]logindSessionIdentity, len(source))
	for id, identity := range source {
		copy[id] = identity
	}
	return copy
}

func deliverLogindLogoff(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, uid uint32) error {
	notification := client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "uid:" + strconv.FormatUint(uint64(uid), 10)}
	select {
	case events <- notification:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("logind lifecycle event queue overflow")
	}
}

func deliverMissedLogoffs(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, previous, current map[string]logindSessionIdentity) error {
	seen := make(map[uint32]bool)
	currentUsers := make(map[uint32]bool)
	for _, identity := range current {
		currentUsers[identity.uid] = true
	}
	for id, identity := range previous {
		if now, exists := current[id]; exists && now == identity || seen[identity.uid] || currentUsers[identity.uid] {
			continue
		}
		if err := deliverLogindLogoff(ctx, events, identity.uid); err != nil {
			return err
		}
		seen[identity.uid] = true
	}
	return nil
}

type logindSession struct {
	conn          *dbus.Conn
	owner         string
	signals       chan *dbus.Signal
	overflow      chan struct{}
	inhibitor     *os.File
	delay         time.Duration
	sleeping      bool
	resumed       bool
	failedSuspend bool
	stopOnCancel  func() bool
	sessions      map[string]logindSessionIdentity
}

type logindSignalHandler struct {
	signals  chan *dbus.Signal
	overflow chan struct{}
}

func (h *logindSignalHandler) DeliverSignal(_ string, _ string, signal *dbus.Signal) {
	select {
	case h.signals <- signal:
	default:
		select {
		case h.overflow <- struct{}{}:
		default:
		}
	}
}

func openLogindSession(ctx context.Context) (_ *logindSession, resultErr error) {
	signals := make(chan *dbus.Signal, 32)
	overflow := make(chan struct{}, 1)
	conn, err := dbus.SystemBusPrivate(dbus.WithSignalHandler(&logindSignalHandler{signals: signals, overflow: overflow}))
	if err != nil {
		return nil, err
	}
	s := &logindSession{conn: conn, signals: signals, overflow: overflow}
	defer func() {
		if resultErr != nil {
			s.close()
		}
	}()
	s.stopOnCancel = context.AfterFunc(ctx, func() { _ = conn.Close() })
	if err := conn.Auth(nil); err != nil {
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		return nil, err
	}
	if !conn.SupportsUnixFDs() {
		return nil, errors.New("system bus does not support inhibitor file descriptors")
	}
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchInterface(busInterface), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, loginName)); err != nil {
		return nil, err
	}
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchInterface(loginInterface), dbus.WithMatchMember("PrepareForSleep"), dbus.WithMatchObjectPath(loginPath)); err != nil {
		return nil, err
	}
	for _, member := range []string{"SessionNew", "SessionRemoved"} {
		if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchInterface(loginInterface), dbus.WithMatchMember(member), dbus.WithMatchObjectPath(loginPath)); err != nil {
			return nil, err
		}
	}
	if err := conn.BusObject().CallWithContext(ctx, busInterface+".GetNameOwner", 0, loginName).Store(&s.owner); err != nil || s.owner == "" {
		return nil, errors.New("logind owner unavailable")
	}
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	var delay dbus.Variant
	if err := conn.Object(loginName, loginPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, loginInterface, "InhibitDelayMaxUSec").Store(&delay); err != nil {
		return nil, err
	}
	usec, ok := delay.Value().(uint64)
	if !ok || usec == 0 || usec > uint64((24*time.Hour)/time.Microsecond) {
		return nil, errors.New("invalid logind inhibitor delay")
	}
	s.delay = time.Duration(usec) * time.Microsecond
	var current string
	if err := conn.BusObject().CallWithContext(ctx, busInterface+".GetNameOwner", 0, loginName).Store(&current); err != nil || current != s.owner {
		return nil, errors.New("logind owner changed during subscription")
	}
	s.sessions, err = s.listSessions(ctx)
	if err != nil {
		return nil, err
	}
	current = ""
	if err := conn.BusObject().CallWithContext(ctx, busInterface+".GetNameOwner", 0, loginName).Store(&current); err != nil || current != s.owner {
		return nil, errors.New("logind owner changed during session inventory")
	}
	return s, nil
}

func (s *logindSession) listSessions(ctx context.Context) (map[string]logindSessionIdentity, error) {
	var rows [][]any
	if err := s.conn.Object(loginName, loginPath).CallWithContext(ctx, loginInterface+".ListSessions", 0).Store(&rows); err != nil {
		return nil, err
	}
	return decodeLogindSessions(rows)
}

func decodeLogindSessions(rows [][]any) (map[string]logindSessionIdentity, error) {
	if len(rows) > 4096 {
		return nil, errors.New("logind session inventory exceeds bound")
	}
	result := make(map[string]logindSessionIdentity, len(rows))
	for _, row := range rows {
		if len(row) != 5 {
			return nil, errors.New("invalid logind session inventory")
		}
		id, idOK := row[0].(string)
		uid, uidOK := row[1].(uint32)
		_, userOK := row[2].(string)
		_, seatOK := row[3].(string)
		path, pathOK := row[4].(dbus.ObjectPath)
		if !idOK || id == "" || !uidOK || !userOK || !seatOK || !pathOK || !path.IsValid() || result[id].path != "" {
			return nil, errors.New("invalid logind session identity")
		}
		result[id] = logindSessionIdentity{uid: uid, path: path}
	}
	return result, nil
}

func removeLogindSession(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, sessions map[string]logindSessionIdentity, id string, path dbus.ObjectPath) error {
	identity, exists := sessions[id]
	if !exists || identity.path != path {
		return errors.New("unbound logind session removal")
	}
	if err := deliverLogindLogoff(ctx, events, identity.uid); err != nil {
		return err
	}
	delete(sessions, id)
	return nil
}

func (s *logindSession) acquire(ctx context.Context) error {
	var fd dbus.UnixFD
	if err := s.conn.Object(loginName, loginPath).CallWithContext(ctx, loginInterface+".Inhibit", 0, "sleep", "endlessnet-client", "close network runtime before sleep", "delay").Store(&fd); err != nil {
		return err
	}
	s.inhibitor = os.NewFile(uintptr(fd), "logind-sleep-inhibitor")
	if s.inhibitor == nil {
		return errors.New("logind returned invalid inhibitor")
	}
	return nil
}

func (s *logindSession) preparing(ctx context.Context) (bool, error) {
	var value dbus.Variant
	if err := s.conn.Object(loginName, loginPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, loginInterface, "PreparingForSleep").Store(&value); err != nil {
		return false, err
	}
	preparing, ok := value.Value().(bool)
	if !ok {
		return false, errors.New("invalid logind sleep state")
	}
	return preparing, nil
}

func (s *logindSession) suspend(ctx context.Context, events chan<- client.RuntimeLifecycleNotification) error {
	deadline := time.Now().Add(s.delay)
	transition, cancel := context.WithCancel(ctx)
	busLifetime := context.Background()
	if s.conn != nil {
		busLifetime = s.conn.Context()
	}
	stopOnBusLoss := context.AfterFunc(busLifetime, cancel)
	err := sendLogindEvent(transition, events, client.RuntimeSuspend, deadline)
	if s.conn != nil {
		err = errors.Join(err, s.conn.Context().Err())
	}
	stopOnBusLoss()
	cancel()
	// A delay inhibitor must remain open until the executor finishes or the
	// configured logind bound expires. Closing it permits logind to sleep.
	if s.inhibitor != nil {
		err = errors.Join(err, s.inhibitor.Close())
		s.inhibitor = nil
	}
	if err != nil {
		s.failedSuspend = true
	}
	return err
}

func (s *logindSession) resume(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, event client.RuntimeLifecycleEvent) error {
	transition, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stopOnBusLoss := context.AfterFunc(s.conn.Context(), cancel)
	defer stopOnBusLoss()
	err := sendLogindEvent(transition, events, event, time.Time{})
	return errors.Join(err, s.conn.Context().Err())
}

func (s *logindSession) listen(ctx context.Context, events chan<- client.RuntimeLifecycleNotification) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.conn.Context().Done():
			return errors.New("system bus disconnected")
		case <-s.overflow:
			return errors.New("logind signal queue overflow")
		case signal := <-s.signals:
			if signal == nil {
				return errors.New("logind signal overflow or malformed delivery")
			}
			switch signal.Name {
			case busInterface + ".NameOwnerChanged":
				if signal.Sender != busInterface || len(signal.Body) != 3 || signal.Body[0] != loginName || signal.Body[1] != s.owner {
					return errors.New("logind owner signal mismatch")
				}
				return errors.New("logind owner changed")
			case loginInterface + ".PrepareForSleep":
				if signal.Sender != s.owner || signal.Path != loginPath || len(signal.Body) != 1 {
					return errors.New("logind sleep signal mismatch")
				}
				preparing, ok := signal.Body[0].(bool)
				if !ok {
					return errors.New("invalid logind sleep signal")
				}
				if preparing {
					if s.sleeping {
						continue
					}
					if s.inhibitor == nil {
						return errors.New("sleep signal without inhibitor")
					}
					if err := s.suspend(ctx, events); err != nil {
						return err
					}
					s.markSleeping()
				} else {
					if !s.sleeping {
						continue
					}
					if s.inhibitor == nil {
						if err := s.acquire(ctx); err != nil {
							return err
						}
					}
					if err := s.resume(ctx, events, client.RuntimeResume); err != nil {
						return err
					}
					s.sleeping = false
					s.resumed = true
				}
			case loginInterface + ".SessionNew", loginInterface + ".SessionRemoved":
				if signal.Sender != s.owner || signal.Path != loginPath || len(signal.Body) != 2 {
					return errors.New("logind session signal mismatch")
				}
				id, idOK := signal.Body[0].(string)
				path, pathOK := signal.Body[1].(dbus.ObjectPath)
				if !idOK || id == "" || !pathOK || !path.IsValid() {
					return errors.New("invalid logind session signal")
				}
				if signal.Name == loginInterface+".SessionNew" {
					current, err := s.listSessions(ctx)
					if err != nil {
						return err
					}
					identity, exists := current[id]
					if !exists || identity.path != path {
						return errors.New("logind session creation not confirmed")
					}
					if prior, exists := s.sessions[id]; exists && prior != identity {
						return errors.New("logind session identity replaced")
					}
					s.sessions[id] = identity
				} else {
					if err := removeLogindSession(ctx, events, s.sessions, id, path); err != nil {
						return err
					}
				}
			}
		}
	}
}

func sendLogindEvent(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, event client.RuntimeLifecycleEvent, deadline time.Time) error {
	completion := make(chan error, 1)
	request := client.RuntimeLifecycleNotification{Event: event, Completion: completion, Deadline: deadline}
	select {
	case events <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case err := <-completion:
			return err
		case <-timer.C:
			return errors.New("logind inhibitor deadline expired")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case err := <-completion:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *logindSession) close() {
	if s.stopOnCancel != nil {
		s.stopOnCancel()
	}
	if s.inhibitor != nil {
		_ = s.inhibitor.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
}
