package main

import (
	"log"
	"educationagent/database_service_go/internal/handler"
)

func main() {
	log.Fatal(handler.Run())
}
