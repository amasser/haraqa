package server

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/haraqa/haraqa/internal/filequeue"
	"github.com/pkg/errors"
)

type Option func(*Server) error

func WithQueue(q Queue) Option {
	return func(s *Server) error {
		if q == nil {
			return errors.New("queue cannot be nil")
		}
		s.q = q
		return nil
	}
}

func WithDirs(dirs ...string) Option {
	return func(s *Server) error {
		if len(dirs) == 0 {
			return errors.New("at least one directory must be given")
		}
		s.dirs = dirs
		return nil
	}
}

func WithMetrics(metrics Metrics) Option {
	return func(s *Server) error {
		if metrics == nil {
			return errors.New("metrics cannot be nil")
		}
		s.metrics = metrics
		return nil
	}
}

func WithDefaultConsumeLimit(n int64) Option {
	return func(s *Server) error {
		if n <= 0 {
			n = -1
		}
		s.defaultLimit = n
		return nil
	}
}

func WithMiddleware(middleware ...mux.MiddlewareFunc) Option {
	return func(s *Server) error {
		s.middlewares = append(s.middlewares, middleware...)
		return nil
	}
}

func WithFileCaching(cache bool) Option {
	return func(s *Server) error {
		s.qFileCache = cache
		return nil
	}
}

func WithFileEntries(entries int64) Option {
	return func(s *Server) error {
		if entries < 0 {
			return errors.New("invalid FileEntries, value must not be negative")
		}
		s.qFileEntries = entries
		return nil
	}
}

type Server struct {
	*mux.Router
	q            Queue
	qFileCache   bool
	qFileEntries int64
	defaultLimit int64
	dirs         []string
	metrics      Metrics
	isClosed     bool
	middlewares  []mux.MiddlewareFunc
}

func NewServer(options ...Option) (*Server, error) {
	s := &Server{
		Router:       mux.NewRouter(),
		metrics:      noOpMetrics{},
		dirs:         []string{".haraqa"},
		defaultLimit: -1,
		qFileCache:   true,
		qFileEntries: 5000,
	}

	for _, option := range options {
		if err := option(s); err != nil {
			return nil, errors.Wrap(err, "invalid option")
		}
	}

	if s.q == nil {
		// default queue
		var err error
		s.q, err = filequeue.New(s.qFileCache, s.qFileEntries, s.dirs...)
		if err != nil {
			return nil, err
		}
	}

	if len(s.middlewares) > 0 {
		s.Router.Use(s.middlewares...)
	}

	s.Router.PathPrefix("/raw/").Handler(http.StripPrefix("/raw/", http.FileServer(http.Dir(s.q.RootDir()))))
	r := s.Router.PathPrefix("/topics").Subrouter()
	r.Path("").Methods(http.MethodGet).Handler(s.HandleGetAllTopics())
	r.Path("/{topic:.*}").Methods(http.MethodPut).Handler(s.HandleCreateTopic())
	r.Path("/{topic:.*}").Methods(http.MethodPatch).Handler(s.HandleModifyTopic())
	r.Path("/{topic:.*}").Methods(http.MethodDelete).Handler(s.HandleDeleteTopic())
	r.Path("/{topic:.*}").Methods(http.MethodPost).Handler(s.HandleProduce())
	r.Path("/{topic:.*}").Methods(http.MethodGet).Handler(s.HandleConsume())
	r.Path("/{topic:.*}").Methods(http.MethodOptions).Handler(s.HandleOptions())

	return s, nil
}

func (s *Server) Close() error {
	s.isClosed = true
	return s.q.Close()
}
