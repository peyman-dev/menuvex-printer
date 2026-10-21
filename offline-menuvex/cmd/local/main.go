package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sinaqaffari/coffish/desktop"
)

func main() {
	app, err := desktop.New(os.Getenv("COFFISH_DATA_DIR"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	addr := os.Getenv("COFFISH_DESKTOP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	fmt.Println("منووکس آفلاین آماده است: http://" + addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      125 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
