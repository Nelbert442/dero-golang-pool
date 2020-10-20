package website

import (
	"log"
	"net/http"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
)

func NewWebsite(cfg *pool.Website) {
	fileServer := http.FileServer(http.Dir("./website/site"))
	http.Handle("/", fileServer)

	// If SSL is enabled, configure for SSL and HTTP. Else just run HTTP
	if cfg.SSL {
		go func() {
			log.Printf("[Website] Starting website at port %v\n", cfg.Port)

			addr := ":" + cfg.Port
			err := http.ListenAndServe(addr, nil)
			if err != nil {
				log.Printf("[Website] Error starting http server at %v", addr)
				log.Fatal(err)
			}
		}()

		log.Printf("[Website] Starting SSL website at port %v\n", cfg.SSLPort)

		addr := ":" + cfg.SSLPort
		err := http.ListenAndServeTLS(addr, cfg.CertFile, cfg.KeyFile, nil)
		if err != nil {
			log.Printf("[Website] Error starting https server at %v", addr)
			log.Fatal(err)
		}
	} else {
		log.Printf("[Website] Starting website at port %v\n", cfg.Port)

		addr := ":" + cfg.Port
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Printf("[Website] Error starting http server at %v", addr)
			log.Fatal(err)
		}
	}
}
