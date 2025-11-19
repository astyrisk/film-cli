package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// shared HTTP client with timeout
var client = &http.Client{
	Timeout: 10 * time.Second,
}

// MediaType is the type of content (movie or tv).
type MediaType string

const (
	Movie MediaType = "movie"
	TV    MediaType = "tv"
)

// ResolveOptions contains the input parameters for resolving an HLS stream.
type ResolveOptions struct {
	IMDBID  string
	Type    MediaType
	Season  int
	Episode int
}

// StreamVariant represents one HLS variant (quality level).
type StreamVariant struct {
	Resolution string
	Bandwidth  string
	URL        string
}

// ResolveVariants runs the full resolution pipeline and returns the final HLS master URL.
func (o ResolveOptions) ResolveVariants() (string, error) {
	log.Println("Starting stream resolution...")

	url, err := o.buildEmbedURL()
	if err != nil {
		return "", err
	}
	log.Printf("Built embed URL: %s", url)

	html, err := fetchEmbedPage(url)
	if err != nil {
		return "", err
	}

	rpcURL, err := extractRPCURLFromHTML(html)
	if err != nil {
		return "", err
	}

	proRPCURL, err := fetchProRPCURL(rpcURL)
	if err != nil {
		return "", err
	}

	hlsURL, err := fetchHLSURL(proRPCURL)
	if err != nil {
		return "", err
	}

	return hlsURL, nil
}

// ResolveStreams fetches the master playlist and extracts all variant streams.
func (o ResolveOptions) ResolveStreams() ([]StreamVariant, error) {
	masterURL, err := o.ResolveVariants()
	if err != nil {
		return nil, err
	}
	log.Printf("Fetching master playlist from: %s", masterURL)

	resp, err := client.Get(masterURL)
	if err != nil {
		return nil, fmt.Errorf("fetching master playlist %q: %w", masterURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for master playlist %q", resp.StatusCode, masterURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading master playlist %q: %w", masterURL, err)
	}

	lines := strings.Split(string(body), "\n")
	var variants []StreamVariant

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			attrs := parseAttributes(line)
			resolution := attrs["RESOLUTION"]
			bandwidth := attrs["BANDWIDTH"]
			if i+1 < len(lines) {
				urlLine := strings.TrimSpace(lines[i+1])
				if urlLine != "" && !strings.HasPrefix(urlLine, "#") {
					abs := resolveRelativeURL(masterURL, urlLine)
					variant := StreamVariant{
						Resolution: resolution,
						Bandwidth:  bandwidth,
						URL:        abs,
					}
					variants = append(variants, variant)
					log.Printf("Found variant: Resolution=%s, Bandwidth=%s", resolution, bandwidth)
				}
			}
		}
	}

	if len(variants) == 0 {
		return nil, fmt.Errorf("no stream variants found in master playlist %q", masterURL)
	}

	log.Printf("Found %d stream variants.", len(variants))
	return variants, nil
}

func (o ResolveOptions) buildEmbedURL() (string, error) {
	const vidsrcBase = "https://vidsrc.net"

	switch o.Type {
	case Movie:
		if o.IMDBID == "" {
			return "", fmt.Errorf("cannot build movie URL: imdbId is empty")
		}
		return fmt.Sprintf("%s/embed/movie?imdb=%s", vidsrcBase, o.IMDBID), nil

	case TV:
		if o.IMDBID == "" {
			return "", fmt.Errorf("cannot build tv URL: imdbId is empty")
		}
		if o.Season == 0 || o.Episode == 0 {
			return "", fmt.Errorf("cannot build tv URL for imdbId %q: season and episode must be set", o.IMDBID)
		}
		return fmt.Sprintf("%s/embed/tv?imdb=%s&season=%d&episode=%d",
			vidsrcBase, o.IMDBID, o.Season, o.Episode), nil

	default:
		return "", fmt.Errorf("unsupported media type %q for imdbId %q", o.Type, o.IMDBID)
	}
}

func fetchEmbedPage(url string) (string, error) {
	log.Printf("Fetching embed page: %s", url)
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetching embed page %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d for embed page %q", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading embed page %q: %w", url, err)
	}
	return string(body), nil
}

func extractRPCURLFromHTML(embedHTML string) (string, error) {
	log.Println("Parsing embed HTML to find iframe source...")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(embedHTML))
	if err != nil {
		return "", fmt.Errorf("parsing embed HTML: %w", err)
	}

	src := doc.Find("iframe").First().AttrOr("src", "")
	if src == "" {
		return "", fmt.Errorf("no iframe src found")
	}
	log.Printf("Found iframe source: %s", src)
	return src, nil
}

func fetchProRPCURL(rpcURL string) (string, error) {
	fullURL := fmt.Sprintf("https:%s", rpcURL)
	log.Printf("Fetching Pro RPC page: %s", fullURL)
	resp, err := client.Get(fullURL)
	if err != nil {
		return "", fmt.Errorf("fetching RPC page %q: %w", rpcURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d for RPC page %q", resp.StatusCode, rpcURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading RPC page body %q: %w", rpcURL, err)
	}

	re := regexp.MustCompile(`src:\s*['"]([^'"]+)['"]`)
	match := re.FindStringSubmatch(string(body))
	if len(match) < 2 {
		return "", fmt.Errorf("no file URL found in RPC page %q", rpcURL)
	}
	log.Printf("Found file URL in Pro RPC page: %s", match[1])
	return match[1], nil
}

func fetchHLSURL(proRPCURL string) (string, error) {
	const cloudnestra = "https://cloudnestra.com"

	requestURL := cloudnestra + proRPCURL
	log.Printf("Fetching HLS URL from: %s", requestURL)
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for HLS page %q: %w", requestURL, err)
	}
	req.Header.Set("Referer", cloudnestra)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching HLS page %q: %w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d for HLS page %q", resp.StatusCode, requestURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading HLS page body %q: %w", requestURL, err)
	}

	re := regexp.MustCompile(`file:\s*['"]([^'"]+)['"]`)
	match := re.FindStringSubmatch(string(body))
	if len(match) < 2 {
		return "", fmt.Errorf("no file URL found in HLS page %q", requestURL)
	}
	log.Printf("Found HLS file URL: %s", match[1])
	return match[1], nil
}

func parseAttributes(line string) map[string]string {
	attrs := map[string]string{}
	parts := strings.Split(line, ",")
	for _, part := range parts {
		if strings.Contains(part, "=") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			key := kv[0]
			val := strings.Trim(kv[1], "\"")
			attrs[key] = val
		}
	}
	return attrs
}

func resolveRelativeURL(baseStr, refStr string) string {
	base, err := url.Parse(baseStr)
	if err != nil {
		return refStr
	}
	ref, err := url.Parse(refStr)
	if err != nil {
		return refStr
	}
	return base.ResolveReference(ref).String()
}

func main() {
	// Example Movie: Iron Man 3 (2013)
	opts := ResolveOptions{
		IMDBID:  "tt1300854", // IMDb ID for the title
		Type:    Movie,       // Movie or TV
		Season:  0,           // only needed for TV
		Episode: 0,           // only needed for TV
	}

	streams, err := opts.ResolveStreams()
	if err != nil {
		log.Fatalf("failed to resolve: %v", err)
	}

	for _, s := range streams {
		fmt.Printf("Resolution: %s | Bandwidth: %s | URL: %s\n",
			s.Resolution, s.Bandwidth, s.URL)
	}

}