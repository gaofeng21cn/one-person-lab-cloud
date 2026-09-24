package main

import (
 "log"
 "os"

 "opl-cloud/services/internal/ownerservice"
)

func main() {
 config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerRuntimeControl, ":8191")
 if err != nil { log.Fatal(err) }
 server, err := ownerservice.NewServer(config)
 if err != nil { log.Fatal(err) }
 server.MarkServing()
 if err := server.Serve(); err != nil { log.Fatal(err) }
}
