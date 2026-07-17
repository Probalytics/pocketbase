// Package main implements a minimal PocketBase executable that serves
// the WorkOS authentication demo UI from ./pb_public.
//
// It is a trimmed down version of examples/base intended for trying out
// the WorkOS integration end-to-end:
//
//	go run ./examples/workos serve
//
// Before starting, enable WorkOS in the app settings (Dashboard >
// Settings, or via the API) and configure the client id, API key and
// webhook secret from your WorkOS dashboard.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func main() {
	app := pocketbase.New()

	// serve the demo UI from the pb_public directory next to the executable
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if !e.Router.HasRoute(http.MethodGet, "/{path...}") {
			e.Router.GET("/{path...}", apis.Static(os.DirFS(publicDir()), true))
		}

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func publicDir() string {
	if len(os.Args) > 0 {
		if fi, err := os.Stat("examples/workos/pb_public"); err == nil && fi.IsDir() {
			return "examples/workos/pb_public"
		}
	}

	return "./pb_public"
}
