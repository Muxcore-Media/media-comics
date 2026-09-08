package internal

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (m *Module) registerComicsHTTPAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/series", m.handleListSeriesHTTP)
	mux.HandleFunc("POST /api/series", m.handleAddSeriesHTTP)
	mux.HandleFunc("GET /api/series/{id}", m.handleGetSeriesHTTP)
	mux.HandleFunc("POST /api/series/{id}/issues", m.handleAddIssueHTTP)
	mux.HandleFunc("PATCH /api/series/{id}", m.handlePatchSeriesHTTP)
	mux.HandleFunc("DELETE /api/series/{id}", m.handleDeleteSeriesHTTP)
	mux.HandleFunc("GET /api/issues", m.handleListIssuesHTTP)
	mux.HandleFunc("PATCH /api/issues/{id}", m.handlePatchIssueHTTP)
	mux.HandleFunc("DELETE /api/issues/{id}", m.handleDeleteIssueHTTP)
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

func (m *Module) handleAddSeriesHTTP(w http.ResponseWriter, r *http.Request) {
	if m.store == nil {
		http.Error(w, `{"error":"store not open"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Title     string `json:"title"`
		Publisher string `json:"publisher"`
		Monitored *bool  `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		http.Error(w, `{"error":"title required"}`, http.StatusBadRequest)
		return
	}
	monitored := true
	if body.Monitored != nil {
		monitored = *body.Monitored
	}
	ser, err := m.store.AddSeries(r.Context(), Series{
		Title: title, Publisher: strings.TrimSpace(body.Publisher), Monitored: monitored,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	writeJSON(w, toSeriesJSON(ser))
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

func (m *Module) handlePatchSeriesHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || m.store == nil {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	mon, ok := readMonitoredJSON(w, r)
	if !ok {
		return
	}
	ser, err := m.store.UpdateSeries(r.Context(), id, SeriesUpdate{Monitored: &mon})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	writeJSON(w, toSeriesJSON(ser))
}

func (m *Module) handleAddIssueHTTP(w http.ResponseWriter, r *http.Request) {
	seriesID := strings.TrimSpace(r.PathValue("id"))
	if seriesID == "" || m.store == nil {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Title     string `json:"title"`
		Number    string `json:"number"`
		Year      int32  `json:"year"`
		Monitored *bool  `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(body.Title)
	number := strings.TrimSpace(body.Number)
	if title == "" && number == "" {
		http.Error(w, `{"error":"title or number required"}`, http.StatusBadRequest)
		return
	}
	monitored := true
	if body.Monitored != nil {
		monitored = *body.Monitored
	}
	iss, err := m.store.AddIssue(r.Context(), Issue{
		SeriesID: seriesID, Title: title, Number: number, Year: body.Year, Monitored: monitored,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	writeJSON(w, toIssueJSON(iss))
}

func (m *Module) handlePatchIssueHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || m.store == nil {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	mon, ok := readMonitoredJSON(w, r)
	if !ok {
		return
	}
	iss, err := m.store.UpdateIssue(r.Context(), id, IssueUpdate{Monitored: &mon})
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

func (m *Module) handleDeleteSeriesHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || m.store == nil {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	deleteFiles := queryDeleteFiles(r)
	if err := m.removeSeries(r.Context(), id, deleteFiles); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	writeJSON(w, map[string]any{"removed": true, "delete_files": deleteFiles})
}

func (m *Module) handleDeleteIssueHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || m.store == nil {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	deleteFiles := queryDeleteFiles(r)
	if err := m.removeIssue(r.Context(), id, deleteFiles); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmtJSONError(err), status)
		return
	}
	writeJSON(w, map[string]any{"removed": true, "delete_files": deleteFiles})
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

func queryDeleteFiles(r *http.Request) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("delete_files"))
	return raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
}

func readMonitoredJSON(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var body struct {
		Monitored *bool `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Monitored == nil {
		http.Error(w, `{"error":"monitored is required"}`, http.StatusBadRequest)
		return false, false
	}
	return *body.Monitored, true
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
