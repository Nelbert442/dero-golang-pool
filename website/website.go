package website

import (
	"log"
	"net/http"
	"os"

	"github.com/Nelbert442/dero-golang-pool/pool"
)

var WebsiteInfoLogger = logFileOutWebsite("INFO")
var WebsiteErrorLogger = logFileOutWebsite("ERROR")

func NewWebsite(cfg *pool.Website) {
	fileServer := http.FileServer(http.Dir("./website/site"))
	http.Handle("/", fileServer)

	// If SSL is enabled, configure for SSL and HTTP. Else just run HTTP
	if cfg.SSL {
		go func() {
			log.Printf("[Website] Starting website at port %v\n", cfg.Port)
			WebsiteInfoLogger.Printf("[Website] Starting website at port %v\n", cfg.Port)

			addr := ":" + cfg.Port
			err := http.ListenAndServe(addr, nil)
			if err != nil {
				log.Printf("[Website] Error starting http server at %v", addr)
				WebsiteErrorLogger.Printf("[Website] Error starting http server at %v", addr)
				WebsiteErrorLogger.Printf("%v", err)
				log.Fatal(err)
			}
		}()

		log.Printf("[Website] Starting SSL website at port %v\n", cfg.SSLPort)
		WebsiteInfoLogger.Printf("[Website] Starting SSL website at port %v\n", cfg.SSLPort)

		addr := ":" + cfg.SSLPort
		err := http.ListenAndServeTLS(addr, cfg.CertFile, cfg.KeyFile, nil)
		if err != nil {
			log.Printf("[Website] Error starting https server at %v", addr)
			WebsiteErrorLogger.Printf("[Website] Error starting https server at %v", addr)
			WebsiteErrorLogger.Printf("%v", err)
			log.Fatal(err)
		}
	} else {
		log.Printf("[Website] Starting website at port %v\n", cfg.Port)
		WebsiteInfoLogger.Printf("[Website] Starting website at port %v\n", cfg.Port)

		addr := ":" + cfg.Port
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Printf("[Website] Error starting http server at %v", addr)
			WebsiteErrorLogger.Printf("[Website] Error starting http server at %v", addr)
			WebsiteErrorLogger.Printf("%v", err)
			log.Fatal(err)
		}
	}
}

func logFileOutWebsite(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/websiteError.log"
	} else {
		logFileName = "logs/website.log"
	}
	os.Mkdir("logs", 0705)
	f, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0705)
	if err != nil {
		panic(err)
	}

	logType := lType + ": "
	l := log.New(f, logType, log.LstdFlags|log.Lmicroseconds)
	return l
}
