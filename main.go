package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
)

const crawlerAgent = "SahilCrawler"

type Page struct {
	URL   string
	Depth int
}

type Result struct {
	Title       string
	Content     string
	Status      int
	FinalURL    string
	ContentType string
	IsHTML      bool
	Links       []string
}

type CrawlResult struct {
	Page   Page
	Result Result
	Err    error
}

func worker(client *http.Client, host string, ticks <-chan time.Time, jobs <-chan Page, results chan<- CrawlResult) {
	for page := range jobs {
		<-ticks
		result, err := fetchPage(client, page.URL, host)
		results <- CrawlResult{Page: page, Result: result, Err: err}
	}
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

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("could not fetch %s", pageURL)
	}
	if header := resp.Header.Get("Content-Type"); header != "" {
		result.ContentType, _, err = mime.ParseMediaType(header)
		if err != nil {
			return result, err
		}
	} else {
		result.ContentType = "text/html"
	}
	if result.ContentType != "text/html" && result.ContentType != "application/xhtml+xml" {
		return result, nil
	}
	result.IsHTML = true

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return result, err
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return result, err
	}

	var mainNode, bodyNode *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				result.Title = strings.Join(strings.Fields(textOf(n)), " ")
			case "main":
				if mainNode == nil {
					mainNode = n
				}
			case "body":
				bodyNode = n
			case "a":
				for _, attr := range n.Attr {
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
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if mainNode != nil {
		result.Content = excerptOf(mainNode)
	}
	if result.Content == "" && bodyNode != nil {
		result.Content = excerptOf(bodyNode)
	}
	return result, nil
}

func textOf(n *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return text.String()
}

func excerptOf(root *html.Node) string {
	var content strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "nav", "header", "footer", "aside":
				return
			}
		}
		if n.Type == html.TextNode {
			appendExcerpt(&content, n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return content.String()
}

func appendExcerpt(dst *strings.Builder, raw string) {
	length := utf8.RuneCountInString(dst.String())
	if length >= 500 {
		return
	}
	words := strings.Join(strings.Fields(raw), " ")
	if words == "" {
		return
	}
	if dst.Len() > 0 {
		dst.WriteByte(' ')
		length++
	}
	for _, r := range words {
		if length >= 500 {
			break
		}
		dst.WriteRune(r)
		length++
	}
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

func searchArchive(path, query string, out io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return err
	}
	columns := make(map[string]int)
	for i, name := range header {
		columns[name] = i
	}
	for _, name := range []string{"url", "title", "content"} {
		if _, ok := columns[name]; !ok {
			return fmt.Errorf("archive is missing %q column", name)
		}
	}

	query = strings.ToLower(query)
	matches := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		title := row[columns["title"]]
		content := row[columns["content"]]
		if strings.Contains(strings.ToLower(title+" "+content), query) {
			matches++
			fmt.Fprintf(out, "%s\n%s\n%s\n\n", title, row[columns["url"]], content)
		}
	}
	fmt.Fprintf(out, "Matches: %d\n", matches)
	return nil
}

func main() {
	startURL := flag.String("url", "https://go.dev/", "starting URL")
	maxPages := flag.Int("max", 5, "maximum pages to visit")
	maxDepth := flag.Int("depth", 1, "maximum link depth")
	workerCount := flag.Int("workers", 3, "maximum concurrent workers")
	output := flag.String("output", "results.csv", "CSV archive path")
	search := flag.String("search", "", "search the saved archive instead of crawling")
	flag.Parse()
	if *search != "" {
		if err := searchArchive(*output, *search, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *maxPages < 1 {
		log.Fatal("max must be at least 1")
	}
	if *maxDepth < 0 {
		log.Fatal("depth cannot be negative")
	}
	if *workerCount < 1 {
		log.Fatal("workers must be at least 1")
	}

	parsedURL, err := url.Parse(*startURL)
	if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") || parsedURL.Hostname() == "" {
		log.Fatal("url must be a full http or https URL")
	}
	parsedURL.Fragment = ""

	file, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"url", "final_url", "title", "content", "status", "depth"}); err != nil {
		log.Fatal(err)
	}

	var robots *robotstxt.RobotsData
	client := &http.Client{
		Timeout: 30 * time.Second,
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
	delay := time.Second
	if group := robots.FindGroup(crawlerAgent); group != nil && group.CrawlDelay > delay {
		delay = group.CrawlDelay
	}
	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	jobs := make(chan Page)
	results := make(chan CrawlResult)
	var workers sync.WaitGroup
	for i := 0; i < *workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			worker(client, parsedURL.Hostname(), ticker.C, jobs, results)
		}()
	}

	queue := []Page{{URL: parsedURL.String(), Depth: 0}}
	seen := map[string]bool{queue[0].URL: true}
	fetchedFinal := make(map[string]bool)
	visited := 0
	inFlight := 0
	htmlPages := 0
	nonHTMLPages := 0
	started := time.Now()

	for (len(queue) > 0 && visited < *maxPages) || inFlight > 0 {
		var next Page
		var jobCh chan Page
		if len(queue) > 0 && visited < *maxPages {
			next = queue[0]
			if fetchedFinal[next.URL] {
				queue = queue[1:]
				fmt.Println("Skipping already fetched:", next.URL)
				continue
			}
			pageURL, err := url.Parse(next.URL)
			if err != nil || !allowedByRobots(robots, pageURL) {
				queue = queue[1:]
				fmt.Println("Skipping disallowed URL:", next.URL)
				continue
			}
			jobCh = jobs
		}

		select {
		case jobCh <- next:
			queue = queue[1:]
			visited++
			inFlight++
			fmt.Printf("\nVisiting (depth %d): %s\n", next.Depth, next.URL)
		case done := <-results:
			inFlight--
			page := done.Page
			result := done.Result
			fmt.Printf("Status for %s: %d\n", page.URL, result.Status)
			if done.Err != nil {
				log.Println(done.Err)
			}
			if err := writer.Write([]string{
				page.URL,
				result.FinalURL,
				result.Title,
				result.Content,
				strconv.Itoa(result.Status),
				strconv.Itoa(page.Depth),
			}); err != nil {
				log.Fatal(err)
			}
			if done.Err != nil {
				continue
			}
			fetchedFinal[result.FinalURL] = true
			if !result.IsHTML {
				nonHTMLPages++
				fmt.Printf("Skipping non-HTML response (%s): %s\n", result.ContentType, page.URL)
				continue
			}
			htmlPages++
			fmt.Println("Links found:", len(result.Links))

			newLinks := 0
			if page.Depth < *maxDepth {
				for _, link := range result.Links {
					if !seen[link] {
						seen[link] = true
						queue = append(queue, Page{URL: link, Depth: page.Depth + 1})
						newLinks++
					}
				}
			}
			fmt.Printf("New links: %d | Pending: %d\n", newLinks, len(queue))
		}
	}
	close(jobs)
	workers.Wait()

	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Fatal(err)
	}

	elapsed := time.Since(started)
	fmt.Println("\nPages attempted:", visited)
	fmt.Println("HTML pages:", htmlPages)
	fmt.Println("Non-HTML pages:", nonHTMLPages)
	fmt.Println("Failed requests:", visited-htmlPages-nonHTMLPages)
	fmt.Printf("Elapsed: %s | Rate: %.2f pages/sec\n", elapsed.Round(time.Millisecond), float64(visited)/elapsed.Seconds())
	fmt.Println("Results saved to", *output)
}
