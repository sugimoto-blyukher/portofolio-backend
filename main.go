package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
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

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errors.New("date must be YYYY-MM-DD")
	}
	return t, nil
}

func main() {
	apiKey := os.Getenv("WAKATIME_API_KEY")
	if apiKey == "" {
		panic("WAKATIME_API_KEY is required")
	}

	r := gin.Default()

	// 自サイトから呼ぶなら CORS 許可が必要（最小例）
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*") // 公開運用ではドメイン固定しろ
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	r.GET("/api/wakatime/languages", func(c *gin.Context) {
		startStr := c.Query("start")
		endStr := c.Query("end")
		if startStr == "" || endStr == "" {
			c.JSON(400, gin.H{"error": "start and end are required"})
			return
		}

		start, err := parseDate(startStr)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		end, err := parseDate(endStr)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if end.Before(start) {
			c.JSON(400, gin.H{"error": "end must be >= start"})
			return
		}

		if end.Sub(start) > 366*24*time.Hour {
			c.JSON(400, gin.H{"error": "range too large (max ~366 days)"})
			return
		}

		timezone := c.DefaultQuery("timezone", "Asia/Tokyo")

		// WakaTime summaries API
		url := fmt.Sprintf(
			"https://api.wakatime.com/api/v1/users/current/summaries?start=%s&end=%s&timezone=%s",
			startStr, endStr, timezone,
		)

		token := base64.StdEncoding.EncodeToString([]byte(apiKey))
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Basic "+token)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(502, gin.H{"error": "failed to reach wakatime"})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			c.JSON(502, gin.H{"error": fmt.Sprintf("wakatime returned %d", resp.StatusCode)})
			return
		}

		var w wakaSummariesResp
		if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
			c.JSON(502, gin.H{"error": "failed to decode wakatime response"})
			return
		}

		agg := map[string]float64{}
		var total float64

		for _, day := range w.Data {
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

		c.JSON(200, gin.H{
			"start":         startStr,
			"end":           endStr,
			"timezone":      timezone,
			"total_seconds": total,
			"languages":     out,
		})
	})

	r.Run(":8080")
}