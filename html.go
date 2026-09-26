package main

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

func main() {
	page := `<h1>Home</h1><a href="/about">About</a><a href="/contact">Contact</a>`
	z := html.NewTokenizer(strings.NewReader(page))

	for {
		if z.Next() == html.ErrorToken {
			break
		}

		tag := z.Token()
		if tag.Type != html.StartTagToken || tag.Data != "a" {
			continue
		}

		for _, attr := range tag.Attr {
			if attr.Key == "href" {
				fmt.Println(attr.Val)
			}
		}
	}
}