package stratum

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Nelbert442/dero-golang-pool/pool"
	"github.com/Nelbert442/dero-golang-pool/util"
)

type Charts struct {
	PoolChartsConfig *pool.PoolChartsConfig
	Api              *ApiServer
}

type ChartData struct {
	Timestamp int64
	Value     int64
}

var ChartsInfoLogger = logFileOutCharts("INFO")
var ChartsErrorLogger = logFileOutCharts("ERROR")

func NewChartsProcessor(pcfg *pool.PoolChartsConfig, a *ApiServer) *Charts {
	c := &Charts{PoolChartsConfig: pcfg, Api: a}
	return c
}

func (c *Charts) Start() {
	log.Printf("[Charts] Starting charts data collection")
	ChartsInfoLogger.Printf("[Charts] Starting charts data collection")
	writeWait, _ := time.ParseDuration("10ms")

	// Pool Hashrate
	if c.PoolChartsConfig.Hashrate.Enabled {
		phrIntv := time.Duration(c.PoolChartsConfig.Interval) * time.Second
		phrTimer := time.NewTimer(phrIntv)
		log.Printf("[Charts] Set pool hashrate chart interval to %v", phrIntv)
		ChartsInfoLogger.Printf("[Charts] Set pool hashrate chart interval to %v", phrIntv)

		go func() {
			for {
				select {
				case <-phrTimer.C:
					stats := c.Api.getStats()
					now := util.MakeTimestamp() / 1000
					if stats["poolHashrate"] == nil {
						phrTimer.Reset(phrIntv)
					} else {
						log.Printf("[Charts] Pool Hashrate: %v", stats["poolHashrate"])
						cData := &ChartData{Timestamp: now, Value: stats["poolHashrate"].(int64)}
						for Graviton_backend.Writing == 1 {
							//log.Printf("[Charts-poolhashrate] GravitonDB is writing... sleeping for %v...", writeWait)
							//StorageInfoLogger.Printf("[Charts-poolhashrate] GravitonDB is writing... sleeping for %v...", writeWait)
							time.Sleep(writeWait)
						}
						Graviton_backend.Writing = 1
						Graviton_backend.WriteChartsData(cData, "poolhashrate", c.PoolChartsConfig.Interval, c.PoolChartsConfig.Hashrate.MaximumPeriod)
						Graviton_backend.Writing = 0
						phrTimer.Reset(phrIntv)
					}
				}
			}
		}()
	}

	// Pool Miners
	if c.PoolChartsConfig.Miners.Enabled {
		pmIntv := time.Duration(c.PoolChartsConfig.Interval) * time.Second
		pmTimer := time.NewTimer(pmIntv)
		log.Printf("[Charts] Set pool miners chart interval to %v", pmIntv)
		ChartsInfoLogger.Printf("[Charts] Set pool miners chart interval to %v", pmIntv)

		go func() {
			for {
				select {
				case <-pmTimer.C:
					stats := c.Api.getStats()
					now := util.MakeTimestamp() / 1000
					if stats["totalPoolMiners"] == nil {
						pmTimer.Reset(pmIntv)
					} else {
						log.Printf("[Charts] Pool Miners: %v", stats["totalPoolMiners"])
						cData := &ChartData{Timestamp: now, Value: stats["totalPoolMiners"].(int64)}
						for Graviton_backend.Writing == 1 {
							//log.Printf("[Charts-totalpoolminers] GravitonDB is writing... sleeping for %v...", writeWait)
							//StorageInfoLogger.Printf("[Charts-totalpoolminers] GravitonDB is writing... sleeping for %v...", writeWait)
							time.Sleep(writeWait)
						}
						Graviton_backend.Writing = 1
						Graviton_backend.WriteChartsData(cData, "totalpoolminers", c.PoolChartsConfig.Interval, c.PoolChartsConfig.Miners.MaximumPeriod)
						Graviton_backend.Writing = 0
						pmTimer.Reset(pmIntv)
					}
				}
			}
		}()
	}

	// Pool Workers
	if c.PoolChartsConfig.Workers.Enabled {
		pwIntv := time.Duration(c.PoolChartsConfig.Interval) * time.Second
		pwTimer := time.NewTimer(pwIntv)
		log.Printf("[Charts] Set pool workers chart interval to %v", pwIntv)
		ChartsInfoLogger.Printf("[Charts] Set pool workers chart interval to %v", pwIntv)

		go func() {
			for {
				select {
				case <-pwTimer.C:
					stats := c.Api.getStats()
					now := util.MakeTimestamp() / 1000
					if stats["totalPoolWorkers"] == nil {
						pwTimer.Reset(pwIntv)
					} else {
						log.Printf("[Charts] Pool Workers: %v", stats["totalPoolWorkers"])
						cData := &ChartData{Timestamp: now, Value: stats["totalPoolWorkers"].(int64)}
						for Graviton_backend.Writing == 1 {
							//log.Printf("[Charts-totalpoolworkers] GravitonDB is writing... sleeping for %v...", writeWait)
							//StorageInfoLogger.Printf("[Charts-totalpoolworkers] GravitonDB is writing... sleeping for %v...", writeWait)
							time.Sleep(writeWait)
						}
						Graviton_backend.Writing = 1
						Graviton_backend.WriteChartsData(cData, "totalpoolworkers", c.PoolChartsConfig.Interval, c.PoolChartsConfig.Workers.MaximumPeriod)
						Graviton_backend.Writing = 0
						pwTimer.Reset(pwIntv)
					}
				}
			}
		}()
	}

	// Pool Difficulty
	if c.PoolChartsConfig.Difficulty.Enabled {
		pdIntv := time.Duration(c.PoolChartsConfig.Interval) * time.Second
		pdTimer := time.NewTimer(pdIntv)
		log.Printf("[Charts] Set pool difficulty chart interval to %v", pdIntv)
		ChartsInfoLogger.Printf("[Charts] Set pool difficulty chart interval to %v", pdIntv)

		go func() {
			for {
				select {
				case <-pdTimer.C:
					// Build lastblock stats
					var diff int64
					now := util.MakeTimestamp() / 1000
					v := c.Api.stratum.rpc()
					prevBlock, getHashERR := v.GetLastBlockHeader()

					if getHashERR != nil {
						log.Printf("[API] Error while retrieving block from node: %v", getHashERR)
						APIErrorLogger.Printf("[API] Error while retrieving block from node: %v", getHashERR)
					} else {
						lastBlock := prevBlock.BlockHeader
						diff, _ = strconv.ParseInt(lastBlock.Difficulty, 10, 64)
					}
					log.Printf("[Charts] Pool Difficulty: %v", diff)
					cData := &ChartData{Timestamp: now, Value: diff}
					for Graviton_backend.Writing == 1 {
						//log.Printf("[Charts-pooldifficulty] GravitonDB is writing... sleeping for %v...", writeWait)
						//StorageInfoLogger.Printf("[Charts-pooldifficulty] GravitonDB is writing... sleeping for %v...", writeWait)
						time.Sleep(writeWait)
					}
					Graviton_backend.Writing = 1
					Graviton_backend.WriteChartsData(cData, "pooldifficulty", c.PoolChartsConfig.Interval, c.PoolChartsConfig.Difficulty.MaximumPeriod)
					Graviton_backend.Writing = 0
					pdTimer.Reset(pdIntv)
				}
			}
		}()
	}
}

func logFileOutCharts(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/chartsError.log"
	} else {
		logFileName = "logs/charts.log"
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
