// Command moodle-mcp serves Zewail City Moodle as MCP tools over stdio.
package main

import (
	"context"
	"log"

	moodle "github.com/OmarAli-prod/moodle-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := moodle.NewServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
