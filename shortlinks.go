package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func FetchShortLinksFromSheet(config AppConfig) map[string]string {
	ctx := context.Background()
	sheetsService, err := sheets.NewService(ctx, option.WithScopes(sheets.SpreadsheetsScope, compute.DevstorageReadOnlyScope))
	if err != nil {
		slog.Error("failed to create http client", slog.Any("error", err))
		return nil
	}

	values, err := sheetsService.Spreadsheets.Values.Get(config.SheetsDocId, fmt.Sprintf("%s!A:B", config.SheetsSheetName)).MajorDimension("ROWS").Do()
	if err != nil {
		slog.Error("failed to get sheet data", slog.Any("error", err))
		return nil
	}

	slog.Debug("fetched values", slog.Any("values", values.Values))

	ret := make(map[string]string)
	for _, link := range values.Values {
		key, keyOk := link[0].(string)
		value, valueOk := link[1].(string)

		if !keyOk || !valueOk {
			continue
		}

		ret[strings.ToLower(key)] = value
	}

	return ret
}

func GetShortLink(c *Cache[string, string], w http.ResponseWriter, r *http.Request) {
	shortlink := r.PathValue("shortlink")
	destination := c.Get("/" + strings.ToLower(shortlink))

	if destination == nil || *destination == "" {
		const FallbackUrl = "https://docs.fimav.us/%s"

		http.Redirect(w, r, fmt.Sprintf(FallbackUrl, shortlink), http.StatusFound)
		return
	}

	slog.Info("Redirecting to shortlink", slog.String("shortlink", shortlink), slog.String("destination", *destination))

	http.Redirect(w, r, *destination, http.StatusFound)
}
