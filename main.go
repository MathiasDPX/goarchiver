package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	warc "github.com/internetarchive/gowarc"
	"golang.org/x/net/html"
)

var (
	// thxx chatgpt
	URL_REGEX    = regexp.MustCompile(`url\(\s*(?:'([^']*)'|"([^"]*)"|([^)\s]+))\s*\)`)
	IMPORT_REGEX = regexp.MustCompile(`@import\s+(?:url\(\s*(?:'([^']*)'|"([^"]*)"|([^)\s]+))\s*\)|'([^']*)'|"([^"]*)")`)
)

func ExtractCSSLinks(body string) []string {
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

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func parseSrcSet(srcset string) []string {
	parts := strings.Split(srcset, ",")
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		fields := strings.Fields(strings.TrimSpace(p))
		if len(fields) > 0 {
			out = append(out, fields[0])
		}
	}
	return out
}

func ExtractHTMLLinks(body string) []string {
	links := make(map[string]struct{})

	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "img", "script", "iframe", "video", "audio", "source":
				if src := getAttr(n, "src"); src != "" {
					links[src] = struct{}{}
				}

				if srcset := getAttr(n, "srcset"); srcset != "" {
					for _, u := range parseSrcSet(srcset) {
						links[u] = struct{}{}
					}
				}
			case "link":
				rel := strings.ToLower(getAttr(n, "rel"))
				if rel != "dns-prefetch" {
					if href := getAttr(n, "href"); href != "" {
						links[href] = struct{}{}
					}
				}
			case "a":
				href := getAttr(n, "href")
				links[href] = struct{}{}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)

	result := make([]string, 0, len(links))
	for link := range links {
		result = append(result, link)
	}

	return result
}

func Archive(client *warc.CustomHTTPClient, url string) []string {
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var links []string
	ct := strings.ToLower(resp.Header.Get("content-type"))
	if strings.HasPrefix(ct, "text/css") {
		links = ExtractCSSLinks(string(body))
	} else if strings.HasPrefix(ct, "text/html") {
		links = ExtractHTMLLinks(string(body))
	}

	<-feedbackChan

	return links
}

func main() {
	rotatorSettings := &warc.RotatorSettings{
		WarcinfoContent: warc.Header{
			"software": "GoArchiver/1.0",
		},
		Compression:        "gzip",
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
		RandomLocalIP:         false,
		DisableIPv6:           true,
		DisableIPv4:           false,
	}

	client, err := warc.NewWARCWritingHTTPClient(clientSettings)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	Archive(client, "https://thevalleyofcode.com/")
}
