package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/stratum"
	"git.dero.io/Nelbert442/dero-golang-pool/website"
)

var cfg pool.Config

func startStratum() {
	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		log.Printf("[Main] Running with %v threads", cfg.Threads)
	} else {
		n := runtime.NumCPU()
		runtime.GOMAXPROCS(n)
		log.Printf("[Main] Running with default %v threads", n)
	}

	s := stratum.NewStratum(&cfg)

	// If API enabled, start api service/listeners
	if cfg.API.Enabled {
		a := stratum.NewApiServer(&cfg.API, s)
		go a.Start()
	}

	// If unlocker enabled, start unlocker processes / go routines
	if cfg.UnlockerConfig.Enabled {
		unlocker := stratum.NewBlockUnlocker(&cfg.UnlockerConfig, s)
		go unlocker.StartBlockUnlocker(s)
	}

	// If payments enabled, start payment processes / go routines
	if cfg.PaymentsConfig.Enabled {
		payments := stratum.NewPayoutsProcessor(&cfg.PaymentsConfig, s)
		payments.Start(s)
	}

	// If website enabled, start website service/listeners
	if cfg.Website.Enabled {
		go website.NewWebsite(&cfg.Website)
	}

	// Listen on defined stratum ports for incoming miners
	s.Listen()
}

func readConfig(cfg *pool.Config) {
	configFileName := "config.json"
	if len(os.Args) > 1 {
		configFileName = os.Args[1]
	}
	configFileName, _ = filepath.Abs(configFileName)
	log.Printf("[Main] Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Fatal("[Main] File error: ", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err = jsonParser.Decode(&cfg); err != nil {
		log.Fatal("[Main] Config error: ", err.Error())
	}
}

func main() {
	/*
		// Declaring globals for use of integrated stuff
		globals.Arguments = make(map[string]interface{})
		globals.Arguments["--debug"] = false
		globals.Arguments["--testnet"] = true

		globals.Initialize()
	*/
	rand.Seed(time.Now().UTC().UnixNano())

	// Read config file every 'ConfigRefreshInterval' interval time for changes, defined in config.json
	readConfig(&cfg)
	/*go func() {
		for {
			interval, _ := time.ParseDuration(cfg.ConfigRefreshInterval)
			timer := time.NewTimer(interval)
			select {
			case <-timer.C:
				readConfig(&cfg)
				timer.Reset(interval)
			}
		}
	}()*/
	startStratum()
}
