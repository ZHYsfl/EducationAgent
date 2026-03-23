package main

import (
	"log"
	"educationagent/pptagentgo/internal/handler"
)

func main() {
	log.Fatal(handler.RunFromEnv())
}
