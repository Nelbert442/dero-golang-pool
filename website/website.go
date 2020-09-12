package website

import (
	"log"
	"net/http"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
)

func NewWebsite(cfg *pool.Website) {
	fileServer := http.FileServer(http.Dir("./website/site"))
	http.Handle("/", fileServer)

	log.Printf("[Website] Starting server at port %v\n", cfg.Port)

	addr := ":" + cfg.Port
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Printf("[Website] Error starting http server at %v", addr)
		log.Fatal(err)
	}
}
