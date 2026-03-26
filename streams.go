package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type StreamCacheKey struct {
	EventCode string
	Year      int64
}

type EventStreamInfo struct {
	Name    string
	Streams []CurrentLiveStream
}

type CurrentLiveStream struct {
	Title      string
	ShortTitle string
	Provider   string
	WatchUrl   string
	EmbedCode  template.HTML
}

type supaCurrentStreams struct {
	Year    int64  `json:"year"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Streams []struct {
		WatchUrl   string `json:"watch_url"`
		EmbedCode  string `json:"embed_code"`
		Platform   string `json:"platform"`
		Title      string `json:"title"`
		ShortTitle string `json:"short_title"`
	} `json:"url"`
}

func FetchLiveStreamsFromDb(config AppConfig, year *int64, onlyCurrent bool) map[StreamCacheKey]EventStreamInfo {
	slog.Info("Fetching Livestreams from DB", slog.Any("year", year))
	client := http.Client{}
	url := config.SupaBaseUrl + "/rest/v1/event_current_stream?order=name"
	if onlyCurrent {
		url += "&is_current=eq." + strconv.FormatBool(onlyCurrent)
	}
	if year != nil {
		url += "&year=eq." + strconv.FormatInt(*year, 10)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Error("Failed to create request for event_current_stream", slog.Any("error", err))
		return nil
	}

	req.Header = http.Header{
		"apiKey":        {config.SupaApiKey},
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer " + config.SupaApiKey},
	}

	res, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to get response for event_current_stream", slog.Any("error", err))
		return nil
	}

	if res.StatusCode != 200 {
		errResp, _ := io.ReadAll(res.Body)
		slog.Error("Bad status for event_current_stream", slog.Int("statusCode", res.StatusCode), slog.String("body", string(errResp)))
		return nil
	}

	var supaCurrentStreams []supaCurrentStreams
	_ = json.NewDecoder(res.Body).Decode(&supaCurrentStreams)

	ret := make(map[StreamCacheKey]EventStreamInfo)
	for _, s := range supaCurrentStreams {
		streams := make([]CurrentLiveStream, len(s.Streams))
		for sIdx, stream := range s.Streams {
			streams[sIdx] = CurrentLiveStream{
				Title:      stream.Title,
				ShortTitle: stream.ShortTitle,
				Provider:   stream.Platform,
				WatchUrl:   stream.WatchUrl,
				EmbedCode:  template.HTML(stream.EmbedCode),
			}
		}
		ret[StreamCacheKey{
			EventCode: strings.ToLower(s.Code),
			Year:      s.Year,
		}] = EventStreamInfo{
			Name:    s.Name,
			Streams: streams,
		}
	}

	return ret
}

func getYearAndEventCode(r *http.Request) (year int64, eventCode string) {
	year, err := strconv.ParseInt(r.PathValue("year"), 10, 0)
	if err != nil {
		year = int64(time.Now().Year())
	}
	eventCode = r.PathValue("eventCode")

	return year, eventCode
}

func GetLiveStreams(c *Cache[StreamCacheKey, EventStreamInfo], w http.ResponseWriter, r *http.Request, templates map[string]*template.Template) {
	year, eventCode := getYearAndEventCode(r)

	streamInfo := c.Get(StreamCacheKey{
		EventCode: strings.ToLower(eventCode),
		Year:      year,
	})

	if streamInfo == nil {
		fallbackToHq(w, r, year, eventCode)
		return
	}

	streams := streamInfo.Streams
	if streams == nil || len(streams) == 0 {
		fallbackToHq(w, r, year, eventCode)
		return
	}

	if len(streams) == 0 {
		err := templates["templates/embed_no_streams.html"].Execute(w, streamInfo)
		if err != nil {
			slog.Error("Failed to execute template for embed_no_streams.html", slog.Any("error", err))
		}
		return
	}

	if len(streams) == 1 {
		http.Redirect(w, r, streams[0].WatchUrl, http.StatusFound)
		return
	}

	err := templates["templates/links_multiple_streams.html"].Execute(w, streamInfo)
	if err != nil {
		slog.Error("Failed to execute template for links_multiple_streams.html", slog.Any("error", err))
	}
	return
}

func fallbackToHq(w http.ResponseWriter, r *http.Request, year int64, eventCode string) {
	// TODO: This needs to properly support FTC
	const FallbackUrl = "https://frc-events.firstinspires.org/%d/%s"

	http.Redirect(w, r, fmt.Sprintf(FallbackUrl, year, eventCode), http.StatusFound)
	return
}

func GetLiveStreamEmbeds(c *Cache[StreamCacheKey, EventStreamInfo], w http.ResponseWriter, r *http.Request, templates map[string]*template.Template) {
	year, eventCode := getYearAndEventCode(r)
	autoplayStr := r.URL.Query().Get("autoplay")
	autoplay, err := strconv.ParseBool(autoplayStr)
	if err != nil {
		autoplay = false
	}

	streamInfo := c.Get(StreamCacheKey{
		EventCode: strings.ToLower(eventCode),
		Year:      year,
	})

	if streamInfo == nil {
		err := templates["templates/embed_no_streams.html"].Execute(w, nil)
		if err != nil {
			slog.Error("Failed to execute template for embed_no_streams.html", slog.Any("error", err))
		}
		return
	}

	streams := streamInfo.Streams

	if autoplay {
		for _, stream := range streams {
			switch strings.ToLower(stream.Provider) {
			case "youtube":
				stream.EmbedCode = stream.EmbedCode
			}
		}
	}

	if streams == nil || len(streams) == 0 {
		err := templates["templates/embed_no_streams.html"].Execute(w, streamInfo)
		if err != nil {
			slog.Error("Failed to execute template for embed_no_streams.html", slog.Any("error", err))
		}
		return
	}

	err = templates["templates/embed_streams.html"].Execute(w, streamInfo)
	if err != nil {
		slog.Error("Failed to execute template for embed_streams.html", slog.Any("error", err))
	}
	return
}

type embedCurrentStreamsData struct {
	Events      []EventStreamInfo
	FeedbackUrl string
}

func GetCurrentLiveStreamEmbeds(c *Cache[int, []EventStreamInfo], config AppConfig, w http.ResponseWriter, templates map[string]*template.Template) {
	events := c.Get(0)

	if events == nil || len(*events) == 0 {
		err := templates["templates/embed_no_events.html"].Execute(w, nil)
		if err != nil {
			slog.Error("Failed to execute template for embed_no_streams.html", slog.Any("error", err))
		}
		return
	}

	err := templates["templates/embed_current_streams.html"].Execute(w, embedCurrentStreamsData{
		Events:      *events,
		FeedbackUrl: config.FeedbackFormUrl,
	})
	if err != nil {
		slog.Error("Failed to execute template for embed_current_streams.html", slog.Any("error", err))
	}
	return
}
