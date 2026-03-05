package wakatime

package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"
)

type wakaSummariesResp struct {
	Data []struct {
		Languages []struct {
			Name         string  `json:"name"`
			TotalSeconds float64 `json:"total_seconds"`
		} `json:"languages"`
	} `json:"data"`
}

type LangAgg struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Percent float64 `json:"percent"`
}

func parseDate(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }

func Handler(w http.ResponseWriter, r *http.Request) {
	// CORS（とりあえず動かす用。本番は Origin を自ドメインに固定しろ）
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		http.Error(w, "start and end are required (YYYY-MM-DD)", 400)
		return
	}
	start, err := parseDate(startStr)
	if err != nil {
		http.Error(w, "start must be YYYY-MM-DD", 400)
		return
	}
	end, err := parseDate(endStr)
	if err != nil {
		http.Error(w, "end must be YYYY-MM-DD", 400)
		return
	}
	if end.Before(start) {
		http.Error(w, "end must be >= start", 400)
		return
	}
	if end.Sub(start) > 366*24*time.Hour {
		http.Error(w, "range too large (max ~366 days)", 400)
		return
	}
	tz := r.URL.Query().Get("timezone")
	if tz == "" {
		tz = "Asia/Tokyo"
	}

	apiKey := os.Getenv("WAKATIME_API_KEY")
	if apiKey == "" {
		http.Error(w, "server missing WAKATIME_API_KEY", 500)
		return
	}

	// WakaTime: API Key を base64 して Basic ヘッダに入れる（username:password じゃない）
	// 公式が「公開サイトでAPIキー使うな」と言ってるので、こういうサーバ側に隠すのが正道。 :contentReference[oaicite:3]{index=3}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKey))

	url := fmt.Sprintf("https://api.wakatime.com/api/v1/users/current/summaries?start=%s&end=%s&timezone=%s",
		startStr, endStr, tz)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", auth)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "failed to reach wakatime", 502)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("wakatime error %d: %s", resp.StatusCode, string(body)), 502)
		return
	}

	var wk wakaSummariesResp
	if err := json.NewDecoder(resp.Body).Decode(&wk); err != nil {
		http.Error(w, "failed to decode wakatime response", 502)
		return
	}

	agg := map[string]float64{}
	var total float64
	for _, day := range wk.Data {
		for _, lang := range day.Languages {
			agg[lang.Name] += lang.TotalSeconds
			total += lang.TotalSeconds
		}
	}

	out := make([]LangAgg, 0, len(agg))
	for name, sec := range agg {
		pct := 0.0
		if total > 0 {
			pct = sec / total * 100
		}
		out = append(out, LangAgg{Name: name, Seconds: sec, Percent: pct})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seconds > out[j].Seconds })

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"start":         startStr,
		"end":           endStr,
		"timezone":      tz,
		"total_seconds": total,
		"languages":     out,
	})
}