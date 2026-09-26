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

func main() {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://example.com",
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Status:", resp.Status)
	fmt.Println("Content-Type:", resp.Header.Get("Content-Type"))

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
			fmt.Println("Link:", fullURL)
		}
	}

	if len(body) > 200 {
		body = body[:200]
	}
	fmt.Println("HTML:", string(body))
}