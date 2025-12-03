package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(proRCPHTML))
	if err != nil {
		return "", fmt.Errorf("parsing ProRCP HTML: %w", err)
	}

	// 1. Extract and Save JS File (optional for direct decoding, but kept for reference)
	scriptSel := doc.Find("script[src*='/sV05kUlNvOdOxvtC/']")
	if scriptSel.Length() > 0 {
		src, exists := scriptSel.First().Attr("src")
		if exists {
			fullURL := "https://cloudnestra.com" + src
			log.Printf("Found JS file URL: %s", fullURL)

			// Fetch content
			jsContent, err := fetchContent(fullURL, "https://cloudnestra.com")
			if err != nil {
				log.Printf("Failed to fetch JS content: %v", err)
			} else {
				// Save to file
				if err := os.MkdirAll("scripts", 0755); err != nil {
					log.Printf("Failed to create scripts directory: %v", err)
				} else {
					scriptPath := "scripts/prorcp.js"
					if err := os.WriteFile(scriptPath, []byte(jsContent), 0644); err != nil {
						log.Printf("Failed to write JS file: %v", err)
					} else {
						log.Println("Saved JS content to scripts/prorcp.js")
					}
				}
			}
		}
	} else {
		log.Println("No script found with src containing /sV05kUlNvOdOxvtC/")
	}

	// 2. Extract Hidden Div Content and ID
	var divContent string
	divSel := doc.Find("div[style='display:none;']")
	if divSel.Length() > 0 {
		divContent = strings.TrimSpace(divSel.First().Text())
		log.Printf("Hidden Div found, length: %d", len(divContent))
	} else {
		log.Println("No hidden div found with style='display:none;'")
		return "", fmt.Errorf("no hidden div found")
	}

	// 3. Decode the content directly
	fmt.Println("DivContent: ")
	fmt.Println(divContent)

	if divContent != "" {
		decodedURL, err := Deobfuscate(divContent)
		if err != nil {
			return "", fmt.Errorf("deobfuscating content: %w", err)
		}
		return decodedURL, nil
	}

	return "", fmt.Errorf("failed to extract necessary components for decoding")
}

// Deobfuscate replicates the logic of the JS function:
// 1. Reverse String -> 2. Take every 2nd char -> 3. Base64 Decode
func Deobfuscate(obfCode string) (string, error) {
	// Convert to rune slice to safely handle characters
	// fmt.Println(obfCode)
	runes := []rune(obfCode)
	n := len(runes)

	// Step 1: Reverse the slice
	for i := 0; i < n/2; i++ {
		runes[i], runes[n-1-i] = runes[n-1-i], runes[i]
	}

	// Step 2: Extract every 2nd character
	// The JS loop was: i starts at 0, increments by 2
	var filtered []rune
	for i := 0; i < n; i += 2 {
		filtered = append(filtered, runes[i])
	}

	filteredStr := string(filtered)

	// Step 3: Base64 Decode
	// We use RawStdEncoding to be permissive, or StdEncoding if padding is standard.
	// Usually, standard StdEncoding is fine.
	decodedBytes, err := base64.StdEncoding.DecodeString(filteredStr)
	if err != nil {
		return "", fmt.Errorf("decoding Base64: %w", err)
	}

	return string(decodedBytes), nil
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
		// IMDBID:  "tt1300854", // IMDb ID for the title
		// IMDBID: "tt30144838",
		IMDBID: "tt0137523",
		// IMDBID: "tt0099685",
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