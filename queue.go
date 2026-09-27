package main

import "fmt"

func main() {
	queue := []string{"page-a"}
	visited := make(map[string]bool)

	links := map[string][]string{
		"page-a": {"page-b", "page-c"},
		"page-b": {"page-a"},
		"page-c": {},
	}

	for len(queue) > 0 {
		page := queue[0]
		queue = queue[1:]

		if visited[page] {
			continue
		}

		visited[page] = true
		fmt.Println("Visiting:", page)

		queue = append(queue, links[page]...)
	}
}