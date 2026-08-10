package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"

	"google.golang.org/grpc"

	"github.com/Muxcore-Media/core/pkg/contracts"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
	comicsv1 "github.com/Muxcore-Media/media-comics/proto/gen/muxcore/comics/v1"
)

type Module struct {
	id, grpcAddr, httpAddr, libraryDir string
	cfgMu                              sync.RWMutex
	store                              *Store
	grpcSrv                            *grpc.Server
	lis                                net.Listener
	httpSrv                            *http.Server
}

type Config struct {
	ID, LibraryDir, GRPCAddr, HTTPAddr string
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
	if v := os.Getenv("COMICS_LIBRARY_DIR"); v != "" {
		cfg.LibraryDir = v
	}
	if v := os.Getenv("MUXCORE_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if cfg.LibraryDir == "" {
		cfg.LibraryDir = "./data/comics"
	}
	return &Module{
		id: cfg.ID, grpcAddr: cfg.GRPCAddr, httpAddr: cfg.HTTPAddr,
		libraryDir: cfg.LibraryDir, store: NewStore(),
	}
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID: m.id, Name: "Manga / Comic Manager", Version: "0.1.0",
		Roles:        []string{"media", "comics"},
		Description:  "Manga/comic series + issue library manager (scaffold)",
		Capabilities: []string{"media.comics", "comics", "manga", "settings"},
		HTTPAddr:     m.grpcAddr,
	}
}

func (m *Module) Init(ctx context.Context) error { return nil }

func (m *Module) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", m.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", m.grpcAddr, err)
	}
	m.lis = lis
	m.grpcSrv = grpc.NewServer()
	comicsv1.RegisterComicManagementServiceServer(m.grpcSrv, &comicServer{m: m})
	modulesdk.RegisterSettings(m.grpcSrv, m.id, m)
	go func() {
		slog.Info("comics gRPC listening", "addr", m.grpcAddr)
		if err := m.grpcSrv.Serve(lis); err != nil {
			slog.Error("gRPC serve", "error", err)
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	m.httpSrv = &http.Server{Addr: m.httpAddr, Handler: mux}
	go func() {
		slog.Info("health listening", "addr", m.httpAddr)
		if err := m.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health serve", "error", err)
		}
	}()
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	if m.httpSrv != nil {
		_ = m.httpSrv.Shutdown(ctx)
	}
	return nil
}

func (m *Module) Health(ctx context.Context) error { return nil }

type comicServer struct {
	comicsv1.UnimplementedComicManagementServiceServer
	m *Module
}

func (s *comicServer) AddSeries(_ context.Context, req *comicsv1.AddSeriesRequest) (*comicsv1.AddSeriesResponse, error) {
	ser, err := s.m.store.AddSeries(Series{
		Title: req.GetTitle(), Publisher: req.GetPublisher(),
		ComicVineID: req.GetComicvineId(), Monitored: req.GetMonitored(), Path: req.GetPath(),
	})
	if err != nil {
		return nil, err
	}
	return &comicsv1.AddSeriesResponse{Series: toPBSeries(ser)}, nil
}

func (s *comicServer) GetSeries(_ context.Context, req *comicsv1.GetSeriesRequest) (*comicsv1.GetSeriesResponse, error) {
	ser, err := s.m.store.GetSeries(req.GetId())
	if err != nil {
		return nil, err
	}
	return &comicsv1.GetSeriesResponse{Series: toPBSeries(ser)}, nil
}

func (s *comicServer) ListSeries(_ context.Context, req *comicsv1.ListSeriesRequest) (*comicsv1.ListSeriesResponse, error) {
	items := s.m.store.ListSeries(req.GetQuery())
	out := make([]*comicsv1.Series, 0, len(items))
	for _, ser := range items {
		out = append(out, toPBSeries(ser))
	}
	return &comicsv1.ListSeriesResponse{Series: out}, nil
}

func (s *comicServer) RemoveSeries(_ context.Context, req *comicsv1.RemoveSeriesRequest) (*comicsv1.RemoveSeriesResponse, error) {
	if err := s.m.store.RemoveSeries(req.GetId()); err != nil {
		return nil, err
	}
	return &comicsv1.RemoveSeriesResponse{Success: true}, nil
}

func (s *comicServer) AddIssue(_ context.Context, req *comicsv1.AddIssueRequest) (*comicsv1.AddIssueResponse, error) {
	iss, err := s.m.store.AddIssue(Issue{
		SeriesID: req.GetSeriesId(), Title: req.GetTitle(),
		Number: req.GetNumber(), Year: req.GetYear(), Monitored: req.GetMonitored(),
	})
	if err != nil {
		return nil, err
	}
	return &comicsv1.AddIssueResponse{Issue: toPBIssue(iss)}, nil
}

func (s *comicServer) ListIssues(_ context.Context, req *comicsv1.ListIssuesRequest) (*comicsv1.ListIssuesResponse, error) {
	items := s.m.store.ListIssues(req.GetSeriesId())
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
