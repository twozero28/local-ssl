package main

import (
	"log"
	"os"

	"local-ssl/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		log.Println("error:", err)
		os.Exit(1)
	}
}
