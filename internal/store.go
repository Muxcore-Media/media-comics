package internal

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type Series struct {
	ID          string
	Title       string
	Publisher   string
	ComicVineID string
	Monitored   bool
	Path        string
}

type Issue struct {
	ID        string
	SeriesID  string
	Title     string
	Number    string
	Year      int32
	Monitored bool
}

type Store struct {
	mu     sync.RWMutex
	series map[string]*Series
	issues map[string]*Issue
}

func NewStore() *Store {
	return &Store{series: map[string]*Series{}, issues: map[string]*Issue{}}
}

func (s *Store) AddSeries(ser Series) (*Series, error) {
	if strings.TrimSpace(ser.Title) == "" {
		return nil, fmt.Errorf("series title required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ser.ID == "" {
		ser.ID = "cs_" + uuid.NewString()[:8]
	}
	cp := ser
	s.series[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (s *Store) GetSeries(id string) (*Series, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ser, ok := s.series[id]
	if !ok {
		return nil, fmt.Errorf("series %q not found", id)
	}
	cp := *ser
	return &cp, nil
}

func (s *Store) ListSeries(query string) []*Series {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]*Series, 0, len(s.series))
	for _, ser := range s.series {
		if q != "" && !strings.Contains(strings.ToLower(ser.Title), q) {
			continue
		}
		cp := *ser
		out = append(out, &cp)
	}
	return out
}

func (s *Store) RemoveSeries(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.series[id]; !ok {
		return fmt.Errorf("series %q not found", id)
	}
	delete(s.series, id)
	for iid, iss := range s.issues {
		if iss.SeriesID == id {
			delete(s.issues, iid)
		}
	}
	return nil
}

func (s *Store) AddIssue(iss Issue) (*Issue, error) {
	if strings.TrimSpace(iss.Number) == "" && strings.TrimSpace(iss.Title) == "" {
		return nil, fmt.Errorf("issue number or title required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.series[iss.SeriesID]; !ok {
		return nil, fmt.Errorf("series %q not found", iss.SeriesID)
	}
	if iss.ID == "" {
		iss.ID = "ci_" + uuid.NewString()[:8]
	}
	cp := iss
	s.issues[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (s *Store) ListIssues(seriesID string) []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Issue, 0)
	for _, iss := range s.issues {
		if seriesID != "" && iss.SeriesID != seriesID {
			continue
		}
		cp := *iss
		out = append(out, &cp)
	}
	return out
}
