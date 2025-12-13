package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/internetarchive/gowarc"
)

var (
	URL_REGEX = regexp.MustCompile(`url\(\s*(['"]?([^'")]+)\1\s*\)`)
	IMPORT_REGEX = regexp.MustCompile(`@import\s+(?:url\()?['"]([^'"]+)['"]\)?`)
)

func ExtractCssLinks(body string) []string {
	links := make(map[string]struct{})

	for _, match := range URL_REGEX.FindAllStringSubmatch(body, -1) {
		links[match[2]] = struct{}{}
	}

	for _, match := range IMPORT_REGEX.FindAllStringSubmatch(body, -1) {
		links[match[1]] = struct{}{}
	}

	result := make([]string, 0, len(links))
	for link := range links {
		result = append(result, link)
	}

	fmt.Println(result)
	return result
}

func Archive(client *warc.CustomHTTPClient, url string) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}

	feedbackChan := make(chan struct{}, 1)
	req = req.WithContext(warc.WithFeedbackChannel(req.Context(), feedbackChan))

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("content-type") == "text/css" {
		ExtractCssLinks(string(resp.Body))
	}

	io.Copy(io.Discard, resp.Body)
	<-feedbackChan
}

func main() {
	rotatorSettings := &warc.RotatorSettings{
		WarcinfoContent: warc.Header{
			"software": "GoArchiver/1.0",
		},
		Compression: "gzip",
		WARCWriterPoolSize: 1,
	}

	clientSettings := warc.HTTPClientSettings{
		RotatorSettings: rotatorSettings,
		DNSServers:      []string{"1.1.1.1", "1.0.0.1"},
		DedupeOptions: warc.DedupeOptions{
			LocalDedupe:   true,
			CDXDedupe:     false,
			SizeThreshold: 2048, // Only payloads above that threshold will be deduped
		},
		DialTimeout:           10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DNSResolutionTimeout:  5 * time.Second,
		DNSRecordsTTL:         5 * time.Minute,
		DNSCacheSize:          10000,
		MaxReadBeforeTruncate: 1000000000,
		DecompressBody:        true,
		FollowRedirects:       true,
		VerifyCerts:           true,
		RandomLocalIP:         true,
	}

	client, err := warc.NewWARCWritingHTTPClient(clientSettings)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	go func() {
		for err := range client.ErrChan {
			fmt.Errorf("WARC writer error: %s", err.Err.Error())
		}
	}()

	Archive(client, "https://thevalleyofcode.com/")
}
