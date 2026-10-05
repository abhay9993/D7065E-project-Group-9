package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/eislab-cps/buildingsim/pkg/client"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:9090", "BuildSim base URL")
	flag.Parse()

	api := client.New(*baseURL)
	building, err := api.Building(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(building.Name)
	for _, level := range building.Levels {
		floor, err := api.Floor(context.Background(), level.ID)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d rooms\n", level.Label, len(floor.Rooms))
	}
}
