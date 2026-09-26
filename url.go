package main

import (
	"fmt"
	"log"
	"net/url"
)

func main() {
	base, err := url.Parse("https://example.com/blog/post")
	if err != nil {
		log.Fatal(err)
	}

	for _, href := range []string{"/about", "next", "https://other.com/page"} {
		link, err := url.Parse(href)
		if err != nil {
			log.Println(err)
			continue
		}

		fmt.Println(href, "->", base.ResolveReference(link))
	}
}