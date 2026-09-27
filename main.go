package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/html"
)

func fetchLinks(client *http.Client, pageURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, pageURL, nil,
	)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Println("Status:", resp.Status)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not fetch %s", pageURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var links []string
	z := html.NewTokenizer(bytes.NewReader(body))

	for {
		if z.Next() == html.ErrorToken {
			break
		}

		tag := z.Token()
		if tag.Type != html.StartTagToken || tag.Data != "a" {
			continue
		}

		for _, attr := range tag.Attr {
			if attr.Key != "href" {
				continue
			}

			link, err := url.Parse(attr.Val)
			if err != nil {
				continue
			}

			fullURL := resp.Request.URL.ResolveReference(link)
			fullURL.Fragment = ""

			if fullURL.Scheme == "https" &&
				fullURL.Hostname() == resp.Request.URL.Hostname() {
				links = append(links, fullURL.String())
			}
		}
	}

	return links, nil
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second}
	queue := []string{"https://go.dev/"}
	seen := map[string]bool{queue[0]: true}
	visited := 0

	for len(queue) > 0 && visited < 5 {
		page := queue[0]
		queue = queue[1:]

		fmt.Println("\nVisiting:", page)
		visited++

		links, err := fetchLinks(client, page)
		if err != nil {
			log.Println(err)
			continue
		}

		fmt.Println("Links found:", len(links))

		newLinks := 0
		for _, link := range links {
			if !seen[link] {
				seen[link] = true
				queue = append(queue, link)
				newLinks++
			}
		}

		fmt.Printf("New links: %d | Pending: %d\n", newLinks, len(queue))
		time.Sleep(time.Second)
	}

	fmt.Println("\nTotal pages visited:", visited)
}