package main

import (
	"context"
	"html/template"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sethvargo/go-envconfig"
)

type AppConfig struct {
	Port            string `env:"PORT, default=8080"`
	SupaBaseUrl     string `env:"SUPA_BASE_URL, required"`
	SupaApiKey      string `env:"SUPA_API_KEY, required"`
	SheetsDocId     string `env:"SHEETS_DOC_ID, required"`
	SheetsSheetName string `env:"SHEETS_SHEET_NAME, required"`
}

func main() {
	var logHandler slog.Handler
	if os.Getenv("LOG_TYPE") == "json" {
		logHandler = slog.NewJSONHandler(os.Stdout, nil)
	} else {
		logHandler = slog.NewTextHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(logHandler))

	var config AppConfig
	if err := envconfig.Process(context.Background(), &config); err != nil {
		slog.Error("Failed to process env vars", slog.Any("error", err))
		os.Exit(1)
	}

	streamCache := NewCache[StreamCacheKey, EventStreamInfo](3*time.Minute, func(requestedKey StreamCacheKey) map[StreamCacheKey]EventStreamInfo {
		return FetchLiveStreamsFromDb(config, &requestedKey.Year, false)
	})
	currentStreamCache := NewCache[int, []EventStreamInfo](3*time.Minute, func(_ int) map[int][]EventStreamInfo {
		streams := FetchLiveStreamsFromDb(config, nil, true)
		ret := make(map[int][]EventStreamInfo)
		ret[0] = slices.Collect(maps.Values(streams))

		return ret
	})
	shortlinkCache := NewCache[string, string](3*time.Minute, func(_ string) map[string]string {
		return FetchShortLinksFromSheet(config)
	})

	// Parse templates once at the start of the application for efficiency.
	// Use `template.Must` to panic if the templates can't be parsed,
	// which is appropriate for application startup.
	partialFiles, _ := filepath.Glob(filepath.Join("templates", "partials", "*.html"))
	layoutTemplate := template.Must(template.Must(template.ParseFiles(filepath.Join("templates", "layouts", "layout.html"))).ParseFiles(partialFiles...))
	templateFiles, _ := filepath.Glob(filepath.Join("templates", "*.html"))
	templates := make(map[string]*template.Template)
	for _, file := range templateFiles {
		templates[file] = template.Must(template.Must(layoutTemplate.Clone()).ParseFiles(file))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/live/{year}/{eventCode}", func(w http.ResponseWriter, r *http.Request) {
		GetLiveStreams(streamCache, w, r, templates)
	})
	mux.HandleFunc("/live/{eventCode}", func(w http.ResponseWriter, r *http.Request) {
		GetLiveStreams(streamCache, w, r, templates)
	})
	mux.HandleFunc("/embed/current", func(w http.ResponseWriter, r *http.Request) {
		GetCurrentLiveStreamEmbeds(currentStreamCache, w, templates)
	})
	mux.HandleFunc("/embed/{year}/{eventCode}", func(w http.ResponseWriter, r *http.Request) {
		GetLiveStreamEmbeds(streamCache, w, r, templates)
	})
	mux.HandleFunc("/embed/{eventCode}", func(w http.ResponseWriter, r *http.Request) {
		GetLiveStreamEmbeds(streamCache, w, r, templates)
	})
	mux.HandleFunc("/{shortlink...}", func(w http.ResponseWriter, r *http.Request) {
		GetShortLink(shortlinkCache, w, r)
	})
	slog.Info("Starting up...", slog.String("url", "http://127.0.0.1:"+config.Port))
	err := http.ListenAndServe(":"+config.Port, mux)
	if err != nil {
		panic(err)
	}
}
