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

	// Step 1: Build and fetch the initial embed page
	embedURL, err := o.buildEmbedURL()
	if err != nil {
		return "", err
	}
	log.Printf("Built embed URL: %s", embedURL)

	embedHTML, err := fetchContent(embedURL, "")
	if err != nil {
		return "", err
	}

	// Step 2: Extract the RCP URL from the iframe
	rcpURL, err := extractRCPURL(embedHTML)
	if err != nil {
		return "", err
	}
	log.Printf("Found RCP URL: %s", rcpURL)

	// Step 3: Fetch the RCP page content
	rcpHTML, err := fetchContent("https:"+rcpURL, "")
	if err != nil {
		return "", err
	}

	// Step 4: Extract the ProRCP URL from the RCP page
	proRCPURL, err := extractProRCPURL(rcpHTML)
	if err != nil {
		return "", err
	}
	log.Printf("Found ProRCP URL: %s", proRCPURL)

	// Step 5: Fetch the ProRCP page with the correct Referer
	proRCPHTML, err := fetchContent("https://cloudnestra.com"+proRCPURL, "https://cloudnestra.com")
	if err != nil {
		return "", err
	}

	// Step 6: Decode the stream URL from the ProRCP page
	hlsURL, err := decodeStreamURL(proRCPHTML)
	if err != nil {
		return "", err
	}
	log.Printf("Decoded HLS URL: %s", hlsURL)

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
	const vidsrcBase = "https://vidsrc-embed.ru" // Updated base URL

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

func fetchContent(url, referer string) (string, error) {
	log.Printf("Fetching page: %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for %q: %w", url, err)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching page %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d for page %q", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading page body %q: %w", url, err)
	}
	return string(body), nil
}

func extractRCPURL(embedHTML string) (string, error) {
	log.Println("Parsing embed HTML to find iframe src for RCP URL...")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(embedHTML))
	if err != nil {
		return "", fmt.Errorf("parsing embed HTML: %w", err)
	}

	src, exists := doc.Find("iframe#player_iframe").Attr("src")
	if !exists || src == "" {
		return "", fmt.Errorf("no iframe src found for RCP URL")
	}
	log.Printf("Found iframe source for RCP: %s", src)
	return src, nil
}

func extractProRCPURL(rcpHTML string) (string, error) {
	log.Println("Extracting ProRCP URL from RCP page...")
	re := regexp.MustCompile(`src: '(/prorcp/[^']+)`)
	match := re.FindStringSubmatch(rcpHTML)
	if len(match) < 2 {
		return "", fmt.Errorf("no ProRCP URL found in RCP page")
	}
	log.Printf("Found ProRCP URL: %s", match[1])
	return match[1], nil
}

func decodeStreamURL(proRCPHTML string) (string, error) {
	log.Println("Decoding stream URL from ProRCP HTML...")
	// TODO: Implement the decoding logic as described in DEVELOPMENT.md.
	// This involves finding the encoded string and the decoding script,
	// then translating the decoding logic from JavaScript to Go.

	// Placeholder for the HLS URL
	// For now, we extract a URL if it exists, but this is not the final logic.
	re := regexp.MustCompile(`file:\s*['"]([^'"]+)['"]`)
	match := re.FindStringSubmatch(proRCPHTML)
	if len(match) < 2 {
		return "", fmt.Errorf("no file URL found in HLS page")
	}

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