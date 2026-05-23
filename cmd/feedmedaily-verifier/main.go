//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowsoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}

	app, err := newVerifierApp(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	app.logger.info("startup", "Starting the Wails verifier window process.", nil)

	runErr := wails.Run(&options.App{
		Title:         "Feed Verification",
		Width:         1160,
		Height:        860,
		MinWidth:      920,
		MinHeight:     640,
		AlwaysOnTop:   true,
		DisableResize: false,
		AssetServer: &assetserver.Options{
			Handler: app.assetHandler(),
		},
		BackgroundColour: options.NewRGB(248, 245, 238),
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Windows: &windowsoptions.Options{
			WebviewUserDataPath: cfg.UserDataDir,
			Theme:               windowsoptions.SystemDefault,
		},
	})
	if runErr != nil {
		app.logger.error("run_failed", "The verifier window process failed before it could finish.", runErr.Error(), nil)
		_ = app.reportFailure(fmt.Sprintf("verification window failed to start: %v", runErr))
		app.logger.info("process_exiting", "The verifier process is exiting after a Wails startup failure.", map[string]any{
			"success": false,
		})
		fmt.Fprintln(os.Stderr, runErr.Error())
		os.Exit(1)
	}
	app.logger.info("process_exiting", "The verifier process is exiting after the Wails app returned.", map[string]any{
		"success": true,
	})
}
