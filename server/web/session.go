package web

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type session struct {
	AdminID        string
	Login          string
	FamilyID       string
	AuthGeneration int64
	CSRF           string
	Expires        time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	lifetime time.Duration
	values   map[string]session
}

func newSessionStore(lifetime time.Duration) *sessionStore {
	return &sessionStore{lifetime: lifetime, values: make(map[string]session)}
}

func (s *sessionStore) create(adminID, login, familyID string, authGeneration int64, now time.Time) (string, session, error) {
	token, err := randomToken()
	if err != nil {
		return "", session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", session{}, err
	}
	value := session{
		AdminID: adminID, Login: login, FamilyID: familyID, AuthGeneration: authGeneration,
		CSRF: csrf, Expires: now.Add(s.lifetime),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, existing := range s.values {
		if !existing.Expires.After(now) {
			delete(s.values, key)
		}
	}
	s.values[token] = value
	return token, value, nil
}

func (s *sessionStore) get(token string, now time.Time) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[token]
	if !ok {
		return session{}, false
	}
	if !value.Expires.After(now) {
		delete(s.values, token)
		return session{}, false
	}
	return value, true
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, token)
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
