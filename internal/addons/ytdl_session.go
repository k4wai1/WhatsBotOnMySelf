package addons

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type ytdlSessionStore struct {
	mu        sync.RWMutex
	downloads map[string]*ytdlSession
	searches  map[string]*ytdlSearchSession
	clock     func() time.Time
}

func NewYtdlSessionStore(clock func() time.Time) *ytdlSessionStore {
	if clock == nil {
		clock = time.Now
	}
	return &ytdlSessionStore{
		downloads: make(map[string]*ytdlSession),
		searches:  make(map[string]*ytdlSearchSession),
		clock:     clock,
	}
}

func (s *ytdlSessionStore) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func (s *ytdlSessionStore) expired(t time.Time) bool {
	age := s.now().Sub(t)
	return age > ytdlSessionTTL || age < 0
}

func (s *ytdlSessionStore) StoreDownload(sender string, session *ytdlSession) {
	if s == nil || session == nil {
		return
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downloads == nil {
		s.downloads = make(map[string]*ytdlSession)
	}
	s.downloads[sender] = session
}

func (s *ytdlSessionStore) StoreSearch(sender string, session *ytdlSearchSession) {
	if s == nil || session == nil {
		return
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searches == nil {
		s.searches = make(map[string]*ytdlSearchSession)
	}
	s.searches[sender] = session
}

// PeekDownload NO consume ni expira la sesión: permite validar la selección.
func (s *ytdlSessionStore) PeekDownload(sender string) (*ytdlSession, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.downloads[sender]
	return session, ok
}

// PeekSearch NO consume ni expira la sesión: permite validar la selección.
func (s *ytdlSessionStore) PeekSearch(sender string) (*ytdlSearchSession, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.searches[sender]
	return session, ok
}

func (s *ytdlSessionStore) TakeDownload(sender string) (*ytdlSession, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.downloads[sender]
	if !ok {
		return nil, false
	}
	delete(s.downloads, sender)
	if s.expired(session.CreatedAt) {
		return nil, false
	}
	return session, true
}

func (s *ytdlSessionStore) TakeSearch(sender string) (*ytdlSearchSession, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.searches[sender]
	if !ok {
		return nil, false
	}
	delete(s.searches, sender)
	if s.expired(session.CreatedAt) {
		return nil, false
	}
	return session, true
}

func (s *ytdlSessionStore) Cancel(sender string) ([]types.MessageID, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]types.MessageID, 0, 2)
	if session, ok := s.downloads[sender]; ok {
		if session.MenuMsgID != "" {
			ids = append(ids, session.MenuMsgID)
		}
		delete(s.downloads, sender)
	}
	if session, ok := s.searches[sender]; ok {
		if session.MenuMsgID != "" {
			ids = append(ids, session.MenuMsgID)
		}
		delete(s.searches, sender)
	}
	return ids, len(ids) > 0
}

func (s *ytdlSessionStore) Cleanup() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for sender, session := range s.downloads {
		if s.expired(session.CreatedAt) {
			delete(s.downloads, sender)
		}
	}
	for sender, session := range s.searches {
		if s.expired(session.CreatedAt) {
			delete(s.searches, sender)
		}
	}
}
