package internal

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (m *Module) registerComicsHTTPAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/series", m.handleListSeriesHTTP)
	mux.HandleFunc("GET /api/series/{id}", m.handleGetSeriesHTTP)
	mux.HandleFunc("GET /api/issues", m.handleListIssuesHTTP)
	mux.HandleFunc("GET /api/missing", m.handleListMissingHTTP)
	mux.HandleFunc("POST /api/scan", m.handleScanHTTP)
	mux.HandleFunc("POST /api/issues/{id}/import", m.handleImportIssueHTTP)
	mux.HandleFunc("GET /api/issues/{id}/stream", m.handleStreamIssueHTTP)
}

func (m *Module) handleListSeriesHTTP(w http.ResponseWriter, r *http.Request) {
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	items, err := m.store.ListSeries(r.Context(), r.URL.Query().Get("q"))
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
	s, err := m.store.GetSeries(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	issues, err := m.store.ListIssues(r.Context(), id)
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
	items, err := m.store.ListIssues(r.Context(), r.URL.Query().Get("series_id"))
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

func (m *Module) handleListMissingHTTP(w http.ResponseWriter, r *http.Request) {
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	items, total, err := m.store.ListMissingIssues(r.Context(), page, pageSize)
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, missingIssuesResponse{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

func (m *Module) handleScanHTTP(w http.ResponseWriter, r *http.Request) {
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	res, err := m.ScanLibrary(r.Context())
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, scanResultJSON{
		FilesFound: res.FilesFound, FilesImported: res.FilesImported,
		FilesSkipped: res.FilesSkipped, PathsCleared: res.PathsCleared,
	})
}

func (m *Module) handleImportIssueHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}
	abs, err := m.resolveLibraryPath(body.Path)
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusBadRequest)
		return
	}
	iss, err := m.store.ImportIssuePath(r.Context(), id, abs)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	writeJSON(w, toIssueJSON(iss))
}

func (m *Module) handleStreamIssueHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	iss, err := m.store.GetIssue(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	if iss.Path == "" {
		http.Error(w, `{"error":"issue has no file"}`, http.StatusNotFound)
		return
	}
	abs, err := m.resolveLibraryPath(iss.Path)
	if err != nil {
		http.Error(w, fmtJSONError(err), http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, abs)
}

type seriesJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Publisher   string `json:"publisher"`
	ComicVineID string `json:"comicvine_id"`
	Path        string `json:"path"`
	Monitored   bool   `json:"monitored"`
}

type issueJSON struct {
	ID        string `json:"id"`
	SeriesID  string `json:"series_id"`
	Title     string `json:"title"`
	Number    string `json:"number"`
	Path      string `json:"path"`
	Year      int32  `json:"year"`
	Monitored bool   `json:"monitored"`
	HasFile   bool   `json:"has_file"`
}

type seriesDetailJSON struct {
	Series seriesJSON  `json:"series"`
	Issues []issueJSON `json:"issues"`
}

type missingIssuesResponse struct {
	Items    []MissingIssue `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type scanResultJSON struct {
	FilesFound    int `json:"files_found"`
	FilesImported int `json:"files_imported"`
	FilesSkipped  int `json:"files_skipped"`
	PathsCleared  int `json:"paths_cleared"`
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
		Number: i.Number, Year: i.Year, Monitored: i.Monitored,
		Path: i.Path, HasFile: issueHasFile(i.Path),
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
