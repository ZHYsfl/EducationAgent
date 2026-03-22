package main

import "multimodal-teaching-agent/internal/server"

func main() {
	app, err := server.InitApp()
	if err != nil {
		panic(err)
	}
	if err = app.Start(); err != nil {
		panic(err)
	}
}
