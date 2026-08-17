package internal

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (m *Module) registerComicsHTTPAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/series", m.handleListSeriesHTTP)
	mux.HandleFunc("GET /api/series/{id}", m.handleGetSeriesHTTP)
	mux.HandleFunc("GET /api/issues", m.handleListIssuesHTTP)
}

func (m *Module) handleListSeriesHTTP(w http.ResponseWriter, r *http.Request) {
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	items, err := m.store.ListSeries(r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusInternalServerError)
		return
	}
	out := make([]seriesJSON, 0, len(items))
	for _, s := range items {
		out = append(out, toSeriesJSON(s))
	}
	writeJSON(w, out)
}

func (m *Module) handleGetSeriesHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	s, err := m.store.GetSeries(id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	issues, err := m.store.ListIssues(id)
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusInternalServerError)
		return
	}
	detail := seriesDetailJSON{Series: toSeriesJSON(s), Issues: make([]issueJSON, 0, len(issues))}
	for _, iss := range issues {
		detail.Issues = append(detail.Issues, toIssueJSON(iss))
	}
	writeJSON(w, detail)
}

func (m *Module) handleListIssuesHTTP(w http.ResponseWriter, r *http.Request) {
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	items, err := m.store.ListIssues(r.URL.Query().Get("series_id"))
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusInternalServerError)
		return
	}
	out := make([]issueJSON, 0, len(items))
	for _, iss := range items {
		out = append(out, toIssueJSON(iss))
	}
	writeJSON(w, out)
}

type seriesJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Publisher   string `json:"publisher"`
	ComicVineID string `json:"comicvine_id"`
	Monitored   bool   `json:"monitored"`
	Path        string `json:"path"`
}

type issueJSON struct {
	ID        string `json:"id"`
	SeriesID  string `json:"series_id"`
	Title     string `json:"title"`
	Number    string `json:"number"`
	Year      int32  `json:"year"`
	Monitored bool   `json:"monitored"`
	Path      string `json:"path"`
}

type seriesDetailJSON struct {
	Series seriesJSON  `json:"series"`
	Issues []issueJSON `json:"issues"`
}

func toSeriesJSON(s *Series) seriesJSON {
	return seriesJSON{
		ID: s.ID, Title: s.Title, Publisher: s.Publisher,
		ComicVineID: s.ComicVineID, Monitored: s.Monitored, Path: s.Path,
	}
}

func toIssueJSON(i *Issue) issueJSON {
	return issueJSON{
		ID: i.ID, SeriesID: i.SeriesID, Title: i.Title,
		Number: i.Number, Year: i.Year, Monitored: i.Monitored, Path: i.Path,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func fmtJSONError(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}
