package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/microsoft/typescript-go/_tsserver/typeeval"
	slogchi "github.com/samber/slog-chi"
)

const (
	poweredByHeader      = "x-powered-by"
	instantiationsHeader = "x-type-instantiations"

	maxRequestBodyBytes = 64 << 10 // 64 KiB
	evalTimeout         = 10 * time.Second
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <app-dir>\n", os.Args[0])
		os.Exit(2)
	}
	appDir := os.Args[1]

	logLevel, logLevelErr := parseLogLevel(os.Getenv("LOG_LEVEL"))
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)
	if logLevelErr != nil {
		logger.Warn("invalid LOG_LEVEL, falling back to info", slog.Any("error", logLevelErr))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	evaluator, err := typeeval.Load(context.Background(), appDir)
	if err != nil {
		logger.Error("failed to load app", slog.String("dir", appDir), slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("loaded type-level app", slog.String("dir", appDir))

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newRouter(logger, evaluator),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("starting server", slog.String("port", port))
	if err := server.ListenAndServe(); err != nil {
		logger.Error("server exited", slog.Any("error", err))
		os.Exit(1)
	}
}

func parseLogLevel(value string) (slog.Level, error) {
	if value == "" {
		return slog.LevelInfo, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return slog.LevelInfo, err
	}
	return level, nil
}

func newRouter(logger *slog.Logger, evaluator *typeeval.Evaluator) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(slogchi.NewWithConfig(logger, slogchi.Config{
		DefaultLevel:     slog.LevelInfo,
		ClientErrorLevel: slog.LevelWarn,
		ServerErrorLevel: slog.LevelError,
		WithUserAgent:    true,
		WithRequestID:    true,
	}))
	r.Handle("/*", serveTypeLevel(logger, evaluator))
	return r
}

func serveTypeLevel(logger *slog.Logger, evaluator *typeeval.Evaluator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Content Too Large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "Bad Request", http.StatusBadRequest)
			}
			return
		}

		headers := make(map[string][]string, len(r.Header))
		for name, values := range r.Header {
			headers[name] = values
		}

		ctx, cancel := context.WithTimeout(r.Context(), evalTimeout)
		defer cancel()
		res, stats, err := evaluator.Evaluate(ctx, typeeval.Request{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: headers,
			Body:    string(body),
		})
		logger.DebugContext(r.Context(), "evaluated type",
			slog.Int("instantiations", stats.Instantiations))
		w.Header().Set(instantiationsHeader, strconv.Itoa(stats.Instantiations))
		if err != nil {
			writeEvalError(w, r, logger, err)
			return
		}

		w.Header().Set(poweredByHeader, "typescript-go/"+typeeval.Version())
		for name, values := range res.Headers {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		w.WriteHeader(res.Status)
		if _, err := io.WriteString(w, res.Body); err != nil {
			logger.WarnContext(r.Context(), "failed to write response", slog.Any("error", err))
		}
	}
}

func writeEvalError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var typeErr *typeeval.TypeError
	var contractErr *typeeval.ContractError
	if errors.As(err, &typeErr) || errors.As(err, &contractErr) {
		logger.ErrorContext(r.Context(), "app implementation error", slog.Any("error", err))
	} else {
		logger.ErrorContext(r.Context(), "evaluation failed", slog.Any("error", err))
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
