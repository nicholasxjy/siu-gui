package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/nicholasxjy/siu-project/internal/core"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "显示版本")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "用法：siu-tui [--version]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println("siu-tui", version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "siu-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	service := core.NewTerminalService(version)
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ui := newTUI(ctx, service)
	ui.refresh()
	return ui.app.Run()
}
