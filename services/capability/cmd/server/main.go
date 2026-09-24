// Command server starts the Capability owner boundary. Product RPC registration
// is added with the owner-local package implementation; until then the process
// exposes only health and the authenticated owner boundary.
package main

import (
	"log"
	"os"

	"opl-cloud/services/internal/ownerservice"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerCapability, ":8181")
	if err != nil {
		log.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		log.Fatal(err)
	}
	server.MarkServing()
	if err := server.Serve(); err != nil {
		log.Fatal(err)
	}
}
