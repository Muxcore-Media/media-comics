package internal

import (
	"context"
	"fmt"
	"log/slog"
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
		cfg.LibraryDir = filepath.Join(cfg.DataDir, "comics")
	}
	return &Module{
		id: cfg.ID, grpcAddr: cfg.GRPCAddr, httpAddr: cfg.HTTPAddr,
		dataDir: cfg.DataDir, libraryDir: cfg.LibraryDir,
	}
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID: m.id, Name: "Manga / Comic Manager", Version: "0.2.0",
		Roles:        []string{"media", "comics"},
		Description:  "Manga/comic series + issue library manager with SQLite persistence",
		Capabilities: []string{"media.comics", "comics", "manga", "settings"},
		HTTPAddr:     m.grpcAddr,
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
	return nil
}

// GRPCListenAddr returns the bound gRPC address after Start.
func (m *Module) GRPCListenAddr() string { return m.grpcAddr }

// HTTPListenAddr returns the bound health/HTTP API address after Start.
func (m *Module) HTTPListenAddr() string { return m.httpAddr }

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
	return nil
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

func (s *comicServer) RemoveSeries(ctx context.Context, req *comicsv1.RemoveSeriesRequest) (*comicsv1.RemoveSeriesResponse, error) {
	if err := s.m.store.RemoveSeries(ctx, req.GetId()); err != nil {
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
	}
}
