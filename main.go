package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
)

const crawlerAgent = "SahilCrawler"

type Page struct {
	URL   string
	Depth int
}

type Result struct {
	Title    string
	Status   int
	FinalURL string
	Links    []string
}

func fetchPage(client *http.Client, pageURL, host string) (Result, error) {
	var result Result

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, pageURL, nil,
	)
	if err != nil {
		return result, err
	}
	req.Header.Set("User-Agent", crawlerAgent)

	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	result.Status = resp.StatusCode
	result.FinalURL = resp.Request.URL.String()
	fmt.Println("Status:", resp.Status)

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("could not fetch %s", pageURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return result, err
	}

	z := html.NewTokenizer(bytes.NewReader(body))
	inTitle := false
	var title strings.Builder

	for {
		if z.Next() == html.ErrorToken {
			break
		}

		token := z.Token()

		if token.Type == html.StartTagToken && token.Data == "title" {
			inTitle = true
			continue
		}
		if token.Type == html.EndTagToken && token.Data == "title" {
			inTitle = false
			continue
		}
		if inTitle && token.Type == html.TextToken {
			title.WriteString(token.Data)
		}

		if token.Type != html.StartTagToken || token.Data != "a" {
			continue
		}

		for _, attr := range token.Attr {
			if attr.Key != "href" {
				continue
			}

			link, err := url.Parse(attr.Val)
			if err != nil {
				continue
			}

			fullURL := resp.Request.URL.ResolveReference(link)
			fullURL.Fragment = ""

			if (fullURL.Scheme == "https" || fullURL.Scheme == "http") &&
				fullURL.Hostname() == host {
				result.Links = append(result.Links, fullURL.String())
			}
		}
	}

	result.Title = strings.Join(strings.Fields(title.String()), " ")
	return result, nil
}

func loadRobots(client *http.Client, root *url.URL) (*robotstxt.RobotsData, error) {
	robotsURL := root.ResolveReference(&url.URL{Path: "/robots.txt"})
	req, err := http.NewRequest(http.MethodGet, robotsURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", crawlerAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("robots.txt exceeds 1 MiB")
	}

	return robotstxt.FromStatusAndBytes(resp.StatusCode, body)
}

func allowedByRobots(robots *robotstxt.RobotsData, pageURL *url.URL) bool {
	path := pageURL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if pageURL.RawQuery != "" {
		path += "?" + pageURL.RawQuery
	}
	return robots.TestAgent(path, crawlerAgent)
}

func main() {
	startURL := flag.String("url", "https://go.dev/", "starting URL")
	maxPages := flag.Int("max", 5, "maximum pages to visit")
	maxDepth := flag.Int("depth", 1, "maximum link depth")
	flag.Parse()

	if *maxPages < 1 {
		log.Fatal("max must be at least 1")
	}
	if *maxDepth < 0 {
		log.Fatal("depth cannot be negative")
	}

	parsedURL, err := url.Parse(*startURL)
	if err != nil ||
		(parsedURL.Scheme != "https" && parsedURL.Scheme != "http") ||
		parsedURL.Hostname() == "" {
		log.Fatal("url must be a full http or https URL")
	}
	parsedURL.Fragment = ""

	file, err := os.Create("results.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{
		"url", "final_url", "title", "status", "depth",
	}); err != nil {
		log.Fatal(err)
	}

	var robots *robotstxt.RobotsData
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Hostname() != parsedURL.Hostname() ||
				(robots != nil && !allowedByRobots(robots, req.URL)) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	robots, err = loadRobots(client, parsedURL)
	if err != nil {
		log.Fatal("could not check robots.txt: ", err)
	}

	queue := []Page{{URL: parsedURL.String(), Depth: 0}}
	seen := map[string]bool{queue[0].URL: true}
	fetchedFinal := make(map[string]bool)
	visited := 0

	for len(queue) > 0 && visited < *maxPages {
		page := queue[0]
		queue = queue[1:]

		if fetchedFinal[page.URL] {
			fmt.Println("Skipping already fetched:", page.URL)
			continue
		}

		pageURL, err := url.Parse(page.URL)
		if err != nil || !allowedByRobots(robots, pageURL) {
			fmt.Println("Skipping disallowed URL:", page.URL)
			continue
		}

		fmt.Printf("\nVisiting (depth %d): %s\n", page.Depth, page.URL)
		visited++

		result, err := fetchPage(client, page.URL, parsedURL.Hostname())
		if err != nil {
			log.Println(err)
		}

		if err := writer.Write([]string{
			page.URL,
			result.FinalURL,
			result.Title,
			strconv.Itoa(result.Status),
			strconv.Itoa(page.Depth),
		}); err != nil {
			log.Fatal(err)
		}
		time.Sleep(time.Second)

		if err != nil {
			continue
		}

		fetchedFinal[result.FinalURL] = true
		fmt.Println("Links found:", len(result.Links))

		newLinks := 0
		if page.Depth < *maxDepth {
			for _, link := range result.Links {
				if !seen[link] {
					seen[link] = true
					queue = append(queue, Page{
						URL:   link,
						Depth: page.Depth + 1,
					})
					newLinks++
				}
			}
		}

		fmt.Printf("New links: %d | Pending: %d\n", newLinks, len(queue))
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("\nTotal pages visited:", visited)
	fmt.Println("Results saved to results.csv")
}
