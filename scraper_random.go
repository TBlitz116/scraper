package main

import (
	"math/rand"
	"net/http"
	"time"
)

var scraperHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// Browser-like User-Agent strings for directory and department fetches.
// Rotating reduces identical fingerprinting across many sequential requests.
var userAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
}

func randomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// newScraperGET builds a GET request with a random User-Agent and typical browser headers.
func newScraperGET(urlStr string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", randomUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	return req, nil
}

// politePause returns a jittered duration in [minMs, maxMs] inclusive.
// Slightly random spacing is gentler than a fixed interval on shared servers.
func politePause(minMs, maxMs int) time.Duration {
	if maxMs < minMs {
		minMs, maxMs = maxMs, minMs
	}
	span := maxMs - minMs + 1
	return time.Duration(minMs+rand.Intn(span)) * time.Millisecond
}
