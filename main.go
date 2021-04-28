package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Nelbert442/dero-golang-pool/pool"
	"github.com/Nelbert442/dero-golang-pool/stratum"
	"github.com/Nelbert442/dero-golang-pool/website"
)

var cfg pool.Config

var MainInfoLogger = logFileOutMain("INFO")
var MainErrorLogger = logFileOutMain("ERROR")

func startStratum() {
	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		log.Printf("[Main] Running with %v threads", cfg.Threads)
		MainInfoLogger.Printf("[Main] Running with %v threads", cfg.Threads)
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

		// Start charts, reliant on api (uses data from api to reduce duplicate db calls/query/processing) and no need to run charts if api isn't running too
		charts := stratum.NewChartsProcessor(&cfg.PoolCharts, &cfg.SoloCharts, a)
		go charts.Start()
	}

	// If EventsConfig is enabled, start event configuration service/listeners
	if cfg.EventsConfig.Enabled {
		events := stratum.NewEventsProcessor(&cfg.EventsConfig, cfg.CoinUnits)
		go events.Start()
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
	MainInfoLogger.Printf("[Main] Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		MainErrorLogger.Printf("[Main] File error: %v", err.Error())
		log.Fatal("[Main] File error: ", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err = jsonParser.Decode(&cfg); err != nil {
		MainErrorLogger.Printf("[Main] Config error: %v", err.Error())
		log.Fatal("[Main] Config error: ", err.Error())
	}
}

func logFileOutMain(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/mainError.log"
	} else {
		logFileName = "logs/main.log"
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

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	rand.Seed(time.Now().UTC().UnixNano())

	readConfig(&cfg)

	startStratum()
}
