package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	warc "github.com/internetarchive/gowarc"
	"golang.org/x/net/html"
)

var (
	// thxx chatgpt
	URL_REGEX    = regexp.MustCompile(`url\(\s*(?:'([^']*)'|"([^"]*)"|([^)\s]+))\s*\)`)
	IMPORT_REGEX = regexp.MustCompile(`@import\s+(?:url\(\s*(?:'([^']*)'|"([^"]*)"|([^)\s]+))\s*\)|'([^']*)'|"([^"]*)")`)

	DOMAIN_WHITELIST = []string{
		"fonts.googleapis.com",
		"fonts.gstatic.com",
	}

	refusedDomains = make(map[string][]string)
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
				if rel != "dns-prefetch" && rel != "preconnect" {
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

func resolveURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}

	if strings.HasPrefix(href, "data:") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "#") {
		return "", false
	}

	if strings.HasPrefix(href, "//") {
		bu, err := url.Parse(baseURL)
		if err != nil || bu.Scheme == "" {
			return "", false
		}

		return bu.Scheme + ":" + href, true
	}

	hu, err := url.Parse(href)
	if err != nil {
		return "", false
	}

	// remove fragment like #
	hu.Fragment = ""

	if hu.IsAbs() {
		if !slices.Contains(DOMAIN_WHITELIST, hu.Host) {
			refusedDomains[hu.Host] = append(refusedDomains[hu.Host], hu.String())
			return "", false
		}
		return hu.String(), true
	}

	bu, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}

	resolved := bu.ResolveReference(hu)

	if !slices.Contains(DOMAIN_WHITELIST, resolved.Host) {
		refusedDomains[resolved.Host] = append(refusedDomains[resolved.Host], resolved.String())
		return "", false
	}

	return resolved.String(), true
}

func Archive(client *warc.CustomHTTPClient, url string) ([]string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var links []string
	ct := strings.ToLower(resp.Header.Get("content-type"))
	if strings.HasPrefix(ct, "text/css") {
		links = ExtractCSSLinks(string(body))
	} else if strings.HasPrefix(ct, "text/html") {
		links = ExtractHTMLLinks(string(body))
	}

	normalized := make([]string, 0, len(links))
	seen := make(map[string]struct{})
	for _, l := range links {
		if abs, ok := resolveURL(url, l); ok {
			if _, dup := seen[abs]; !dup {
				seen[abs] = struct{}{}
				normalized = append(normalized, abs)
			}
		}
	}

	return normalized, nil
}

func main() {
	var whitelist string
	var start string
	var report bool

	flag.StringVar(&whitelist, "whitelist", "", "Whitelist domains")
	flag.StringVar(&start, "start", "", "Starting URLs")
	flag.BoolVar(&report, "report", false, "Show report of refused domains")
	flag.Parse()

	DOMAIN_WHITELIST = strings.Split(whitelist, ",")
	startList := strings.Split(start, ",")

	if len(start) == 0 {
		fmt.Println("No starting points found")
		return
	}

	if len(whitelist) == 0 {
		fmt.Println("No domain whitelisted")
		return
	}

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

	queue := startList
	seen := make(map[string]struct{})
	enqueued := make(map[string]struct{})
	enqueued[queue[0]] = struct{}{}

	for len(queue) > 0 {
		element := queue[0]
		queue = queue[1:]

		if _, ok := seen[element]; ok {
			continue
		}

		links, err := Archive(client, element)

		if err != nil {
			fmt.Printf("Archive error: %v\n", err)
		}

		for _, l := range links {
			if _, wasQueued := enqueued[l]; wasQueued {
				continue
			}
			queue = append(queue, l)
			enqueued[l] = struct{}{}
		}

		seen[element] = struct{}{}

		fmt.Printf("[%d] Archived %s\n", len(queue), element)
	}

	if report && len(refusedDomains) > 0 {
		fmt.Println("\nRefused domains from whitelist:")
		for domain, urls := range refusedDomains {
			fmt.Printf("- %s (%d times)\n", domain, len(urls))
			for _, u := range urls {
				fmt.Printf("    - %s\n", u)
			}
		}
	}
}
