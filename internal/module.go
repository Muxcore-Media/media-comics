package internal

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/Muxcore-Media/core/pkg/contracts"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
	comicsv1 "github.com/Muxcore-Media/media-comics/proto/gen/muxcore/comics/v1"
)

// clampInt32 converts n to int32, saturating at the int32 bounds.
func clampInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n) //nolint:gosec // bounds checked above
}

type Module struct {
	lis        net.Listener
	store      *Store
	grpcSrv    *grpc.Server
	httpSrv    *http.Server
	id         string
	grpcAddr   string
	httpAddr   string
	dataDir    string
	libraryDir string
	imageDir   string
	cfgMu      sync.RWMutex
}

type Config struct {
	ID         string
	DataDir    string
	LibraryDir string
	GRPCAddr   string
	HTTPAddr   string
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "media-comics"
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = ":9660"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":9661"
	}
	if v := os.Getenv("COMICS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("COMICS_LIBRARY_DIR"); v != "" {
		cfg.LibraryDir = v
	}
	if v := os.Getenv("MUXCORE_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.LibraryDir == "" {
		cfg.LibraryDir = cfg.DataDir
	}
	imageDir := os.Getenv("COMICS_IMAGE_DIR")
	if imageDir == "" {
		imageDir = filepath.Join(cfg.DataDir, "images")
	}
	return &Module{
		id: cfg.ID, grpcAddr: cfg.GRPCAddr, httpAddr: cfg.HTTPAddr,
		dataDir: cfg.DataDir, libraryDir: cfg.LibraryDir, imageDir: imageDir,
	}
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID: m.id, Name: "Manga / Comic Manager", Version: "0.2.0",
		Roles:        []string{"media", "comics"},
		Description:  "Manga/comic series + issue library manager with SQLite persistence",
		Capabilities: []string{"media.comics", "comics", "manga", "settings"},
		HTTPAddr:     m.httpAddr,
	}
}

func (m *Module) Init(ctx context.Context) error {
	if err := os.MkdirAll(m.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(m.libraryDir, 0o700); err != nil {
		return fmt.Errorf("create library dir: %w", err)
	}
	dbPath := filepath.Join(m.dataDir, "comics.db")
	store, err := OpenStore(ctx, dbPath)
	if err != nil {
		return err
	}
	m.store = store
	slog.Info("comics library store open", "db", dbPath, "library", m.libraryDir)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if m.store == nil {
		return fmt.Errorf("store not initialized")
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", m.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", m.grpcAddr, err)
	}
	m.lis = lis
	m.grpcAddr = lis.Addr().String()
	m.grpcSrv = grpc.NewServer()
	comicsv1.RegisterComicManagementServiceServer(m.grpcSrv, &comicServer{m: m})
	modulesdk.RegisterSettings(m.grpcSrv, m.id, m)
	go func() {
		slog.Info("comics gRPC listening", "addr", m.grpcAddr)
		if serveErr := m.grpcSrv.Serve(lis); serveErr != nil {
			slog.Error("gRPC serve", "error", serveErr)
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	m.registerComicsHTTPAPI(mux)
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/images/", http.FileServer(http.Dir(m.getImageDir()))).ServeHTTP(w, r)
	})
	httpLis, err := lc.Listen(ctx, "tcp", m.httpAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP %s: %w", m.httpAddr, err)
	}
	m.httpAddr = httpLis.Addr().String()
	m.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("health listening", "addr", m.httpAddr)
		if serveErr := m.httpSrv.Serve(httpLis); serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("health serve", "error", serveErr)
		}
	}()
	if _, err := m.ScanLibrary(ctx); err != nil {
		return fmt.Errorf("startup library scan: %w", err)
	}
	return nil
}

// GRPCListenAddr returns the bound gRPC address after Start.
func (m *Module) GRPCListenAddr() string { return m.grpcAddr }

// HTTPListenAddr returns the bound health/HTTP API address after Start.
func (m *Module) HTTPListenAddr() string { return m.httpAddr }

func (m *Module) libraryRoot() string {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.libraryDir
}

func (m *Module) resolveLibraryPath(path string) (string, error) {
	return pathUnderRoot(path, m.libraryRoot())
}

func (m *Module) deleteFileIfInLibrary(path string) error {
	if path == "" {
		return nil
	}
	abs, err := m.resolveLibraryPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs) //nolint:gosec // abs was validated to lie under the library root by resolveLibraryPath
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) { //nolint:gosec // abs was validated to lie under the library root by resolveLibraryPath
		return fmt.Errorf("delete file %s: %w", abs, err)
	}
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	if m.httpSrv != nil {
		_ = m.httpSrv.Shutdown(ctx)
	}
	if m.store != nil {
		_ = m.store.Close()
		m.store = nil
	}
	return nil
}

func (m *Module) Health(ctx context.Context) error {
	if m.store == nil {
		return fmt.Errorf("store not open")
	}
	return m.store.Ping(ctx)
}

// ScanLibrary scans the configured library root into SQLite.
func (m *Module) ScanLibrary(ctx context.Context) (*ScanResult, error) {
	m.cfgMu.RLock()
	root := m.libraryDir
	store := m.store
	m.cfgMu.RUnlock()
	if store == nil {
		return nil, fmt.Errorf("store not open")
	}
	return store.ScanLibraryRoot(ctx, root)
}

type comicServer struct {
	comicsv1.UnimplementedComicManagementServiceServer
	m *Module
}

func (s *comicServer) AddSeries(ctx context.Context, req *comicsv1.AddSeriesRequest) (*comicsv1.AddSeriesResponse, error) {
	ser, err := s.m.store.AddSeries(ctx, Series{
		Title: req.GetTitle(), Publisher: req.GetPublisher(),
		ComicVineID: req.GetComicvineId(), Monitored: req.GetMonitored(), Path: req.GetPath(),
	})
	if err != nil {
		return nil, err
	}
	return &comicsv1.AddSeriesResponse{Series: toPBSeries(ser)}, nil
}

func (s *comicServer) GetSeries(ctx context.Context, req *comicsv1.GetSeriesRequest) (*comicsv1.GetSeriesResponse, error) {
	ser, err := s.m.store.GetSeries(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &comicsv1.GetSeriesResponse{Series: toPBSeries(ser)}, nil
}

func (s *comicServer) ListSeries(ctx context.Context, req *comicsv1.ListSeriesRequest) (*comicsv1.ListSeriesResponse, error) {
	items, err := s.m.store.ListSeries(ctx, req.GetQuery())
	if err != nil {
		return nil, err
	}
	out := make([]*comicsv1.Series, 0, len(items))
	for _, ser := range items {
		out = append(out, toPBSeries(ser))
	}
	return &comicsv1.ListSeriesResponse{Series: out}, nil
}

func (s *comicServer) UpdateSeries(ctx context.Context, req *comicsv1.UpdateSeriesRequest) (*comicsv1.UpdateSeriesResponse, error) {
	upd := SeriesUpdate{}
	if req.Title != nil {
		upd.Title = req.Title
	}
	if req.Publisher != nil {
		upd.Publisher = req.Publisher
	}
	if req.Monitored != nil {
		upd.Monitored = req.Monitored
	}
	if req.Path != nil {
		upd.Path = req.Path
	}
	ser, err := s.m.store.UpdateSeries(ctx, req.GetId(), upd)
	if err != nil {
		return nil, err
	}
	return &comicsv1.UpdateSeriesResponse{Series: toPBSeries(ser)}, nil
}

func (m *Module) removeSeries(ctx context.Context, id string, deleteFiles bool) error {
	if m.store == nil {
		return fmt.Errorf("store not open")
	}
	if deleteFiles {
		ser, err := m.store.GetSeries(ctx, id)
		if err != nil {
			return err
		}
		paths, err := m.store.ListSeriesIssuePaths(ctx, id)
		if err != nil {
			return err
		}
		for _, p := range paths {
			if err := m.deleteFileIfInLibrary(p); err != nil {
				return err
			}
		}
		if ser.Path != "" {
			if err := m.deleteFileIfInLibrary(ser.Path); err != nil {
				return err
			}
		}
	}
	return m.store.RemoveSeries(ctx, id)
}

func (s *comicServer) RemoveSeries(ctx context.Context, req *comicsv1.RemoveSeriesRequest) (*comicsv1.RemoveSeriesResponse, error) {
	if err := s.m.removeSeries(ctx, req.GetId(), req.GetDeleteFiles()); err != nil {
		return nil, err
	}
	return &comicsv1.RemoveSeriesResponse{Success: true}, nil
}

func (s *comicServer) AddIssue(ctx context.Context, req *comicsv1.AddIssueRequest) (*comicsv1.AddIssueResponse, error) {
	iss, err := s.m.store.AddIssue(ctx, Issue{
		SeriesID: req.GetSeriesId(), Title: req.GetTitle(),
		Number: req.GetNumber(), Year: req.GetYear(), Monitored: req.GetMonitored(),
	})
	if err != nil {
		return nil, err
	}
	return &comicsv1.AddIssueResponse{Issue: toPBIssue(iss)}, nil
}

func (s *comicServer) GetIssue(ctx context.Context, req *comicsv1.GetIssueRequest) (*comicsv1.GetIssueResponse, error) {
	iss, err := s.m.store.GetIssue(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &comicsv1.GetIssueResponse{Issue: toPBIssue(iss)}, nil
}

func (s *comicServer) ListIssues(ctx context.Context, req *comicsv1.ListIssuesRequest) (*comicsv1.ListIssuesResponse, error) {
	items, err := s.m.store.ListIssues(ctx, req.GetSeriesId())
	if err != nil {
		return nil, err
	}
	out := make([]*comicsv1.Issue, 0, len(items))
	for _, iss := range items {
		out = append(out, toPBIssue(iss))
	}
	return &comicsv1.ListIssuesResponse{Issues: out}, nil
}

func (s *comicServer) UpdateIssue(ctx context.Context, req *comicsv1.UpdateIssueRequest) (*comicsv1.UpdateIssueResponse, error) {
	upd := IssueUpdate{}
	if req.Title != nil {
		upd.Title = req.Title
	}
	if req.Number != nil {
		upd.Number = req.Number
	}
	if req.Year != nil {
		upd.Year = req.Year
	}
	if req.Monitored != nil {
		upd.Monitored = req.Monitored
	}
	if req.Path != nil {
		upd.Path = req.Path
	}
	iss, err := s.m.store.UpdateIssue(ctx, req.GetId(), upd)
	if err != nil {
		return nil, err
	}
	return &comicsv1.UpdateIssueResponse{Issue: toPBIssue(iss)}, nil
}

func (m *Module) removeIssue(ctx context.Context, id string, deleteFiles bool) error {
	if deleteFiles {
		iss, err := m.store.GetIssue(ctx, id)
		if err != nil {
			return err
		}
		if err := m.deleteFileIfInLibrary(iss.Path); err != nil {
			return err
		}
	}
	return m.store.RemoveIssue(ctx, id)
}

func (s *comicServer) RemoveIssue(ctx context.Context, req *comicsv1.RemoveIssueRequest) (*comicsv1.RemoveIssueResponse, error) {
	if err := s.m.removeIssue(ctx, req.GetId(), req.GetDeleteFiles()); err != nil {
		return nil, err
	}
	return &comicsv1.RemoveIssueResponse{Success: true}, nil
}

func (s *comicServer) ScanLibrary(ctx context.Context, _ *comicsv1.ScanLibraryRequest) (*comicsv1.ScanLibraryResponse, error) {
	res, err := s.m.ScanLibrary(ctx)
	if err != nil {
		return nil, err
	}
	return &comicsv1.ScanLibraryResponse{
		FilesFound: clampInt32(res.FilesFound), FilesImported: clampInt32(res.FilesImported),
		FilesSkipped: clampInt32(res.FilesSkipped), PathsCleared: clampInt32(res.PathsCleared),
	}, nil
}

func (s *comicServer) ListMissing(ctx context.Context, req *comicsv1.ListMissingRequest) (*comicsv1.ListMissingResponse, error) {
	page := int(req.GetPage())
	pageSize := int(req.GetPageSize())
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	items, total, err := s.m.store.ListMissingIssues(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]*comicsv1.MissingIssue, 0, len(items))
	for _, item := range items {
		out = append(out, &comicsv1.MissingIssue{
			IssueId: item.IssueID, SeriesId: item.SeriesID, Title: item.Title,
			Number: item.Number, SeriesName: item.SeriesName, Year: item.Year,
		})
	}
	return &comicsv1.ListMissingResponse{
		Items: out, Total: clampInt32(total), Page: clampInt32(page), PageSize: clampInt32(pageSize),
	}, nil
}

func (s *comicServer) ImportIssue(ctx context.Context, req *comicsv1.ImportIssueRequest) (*comicsv1.ImportIssueResponse, error) {
	abs, err := s.m.resolveLibraryPath(req.GetPath())
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("import path: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("import path is a directory")
	}
	iss, err := s.m.store.ImportIssuePath(ctx, req.GetIssueId(), abs)
	if err != nil {
		return nil, err
	}
	return &comicsv1.ImportIssueResponse{Issue: toPBIssue(iss)}, nil
}

func toPBSeries(s *Series) *comicsv1.Series {
	return &comicsv1.Series{
		Id: s.ID, Title: s.Title, Publisher: s.Publisher,
		ComicvineId: s.ComicVineID, Monitored: s.Monitored, Path: s.Path,
	}
}

func toPBIssue(i *Issue) *comicsv1.Issue {
	return &comicsv1.Issue{
		Id: i.ID, SeriesId: i.SeriesID, Title: i.Title,
		Number: i.Number, Year: i.Year, Monitored: i.Monitored,
		Path: i.Path, HasFile: issueHasFile(i.Path),
	}
}
