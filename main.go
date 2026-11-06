package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"github.com/ExeQue/dbro/internal/mcpserver"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

var version = "dev"

const usage = `dbro: a fast database client for developers.

Usage:
  dbro            open the app
  dbro mcp        serve saved connections to agents over MCP (stdio)
  dbro mcp --http [--addr 127.0.0.1:7811]
                  serve MCP over streamable HTTP at /mcp
  dbro serve [--addr 127.0.0.1:7810]
                  headless: the app in a web browser, plus MCP at /mcp
  dbro version    print the version

Beyond loopback a token is required: set DBRO_TOKEN, or dbro prints a fresh one.

Add to Claude Code:
  claude mcp add dbro -- dbro mcp
  claude mcp add --transport http dbro http://127.0.0.1:7811/mcp
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp":
			run := func() error { return mcpserver.Run(context.Background(), version) }
			if len(os.Args) > 2 && os.Args[2] == "--http" {
				run = func() error { return serveMCP(os.Args[3:]) }
			}
			if err := run(); err != nil {
				fmt.Fprintln(os.Stderr, "dbro mcp:", err)
				os.Exit(1)
			}
			return
		case "serve":
			if err := serveHeadless(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "dbro serve:", err)
				os.Exit(1)
			}
			return
		case "version", "--version":
			fmt.Println(version)
			return
		case "help", "--help", "-h":
			fmt.Print(usage)
			return
		}
	}

	// WebKitGTK on Wayland with fractional scaling keeps showing a stretched old frame
	// while a tiled window resizes; software compositing relayouts immediately.
	for k, v := range map[string]string{"WEBKIT_DISABLE_COMPOSITING_MODE": "1", "WEBKIT_DISABLE_DMABUF_RENDERER": "1"} {
		if _, ok := os.LookupEnv(k); !ok {
			os.Setenv(k, v)
		}
	}

	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "dbro",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 26, G: 18, B: 20, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       func(context.Context) { unlockWindowSize() },
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Linux: &linux.Options{
			Icon:        icon,
			ProgramName: "dbro",
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
