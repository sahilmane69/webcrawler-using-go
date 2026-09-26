package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type boringResponse struct {
	Activity      string  `json:"activity"`
	Availability  float64 `json:"availability"`
	Type          string  `json:"type"`
	Participants  int     `json:"participants"`
	Price         float64 `json:"price"`
	Accessibility string  `json:"accessibility"`
	Duration      string  `json:"duration"`
	KidFriendly   bool    `json:"kidFriendly"`
	Link          string  `json:"link"`
	Key           string  `json:"key"`
}

func main() {
	ctx := context.Background()
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://bored-api.appbrewery.com/random", nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	var response boringResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%+v\n", response)
}