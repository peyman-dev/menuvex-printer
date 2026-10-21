// Native Windows/macOS/Linux shell; HTMX talks only to the local Go handler.
package main

import (
	"log"
	"net/http"
	"os"

	desktop "github.com/sinaqaffari/coffish/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	app, err := desktop.New(os.Getenv("COFFISH_DATA_DIR"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	handler := app.Handler()
	err = wails.Run(&options.App{
		Title:     "منووکس — سامانه جامع مدیریت کافه و رستوران",
		Width:     1380,
		Height:    880,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Handler: handler,
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("HX-Request") == "true" {
						handler.ServeHTTP(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
