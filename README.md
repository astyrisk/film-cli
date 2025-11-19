# VidSrc Resolver

*This project is currently under development*

This Go program finds streaming links for movies and TV shows. It gets these links from `vidsrc-embed.ru`.

## What It Does

-   Finds streams for movies and TV shows.
-   Gets a list of all quality levels, like 1080p or 720p.

## How to Use

You need to give the program an IMDb ID. For TV shows, you also need the season and episode number.

### Example

Here is how to find streams for a movie in [`main.go`](main.go:274):

```go
func main() {
	opts := ResolveOptions{
		IMDBID:  "tt1300854", // Iron Man 3
		Type:    Movie,
	}

	streams, err := opts.ResolveStreams()
	if err != nil {
		log.Fatalf("failed to resolve: %v", err)
	}

	for _, s := range streams {
		fmt.Printf("Found stream: %s\n", s.URL)
	}
}
```

Run the program from your terminal:

```bash
go run main.go
```
See [`DEVELOPMENT.md`](DEVELOPMENT.md) for more technical details.
