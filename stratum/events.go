package stratum

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Nelbert442/dero-golang-pool/pool"
	"github.com/Nelbert442/dero-golang-pool/util"
)

type Events struct {
	EventsConfig *pool.EventsConfig
	CoinUnits    int64
}

type EventData struct {
	Timestamp int64
	Value     int64
}

type RandomRewardEventDays struct {
	EventDayDetails map[int64]*RandomRewardEventMiners
}

type RandomRewardEventMiners struct {
	MinerDetails map[string]*ApiMiner
}

var EventsInfoLogger = logFileOutCharts("INFO")
var EventsErrorLogger = logFileOutCharts("ERROR")

func NewEventsProcessor(eventsconfig *pool.EventsConfig, coinunits int64) *Events {
	e := &Events{EventsConfig: eventsconfig, CoinUnits: coinunits}
	return e
}

func (e *Events) Start() {
	log.Printf("[Events] Starting events data collection")
	EventsInfoLogger.Printf("[Events] Starting events data collection")
	writeWait, _ := time.ParseDuration("10ms")
	_ = writeWait

	// RandomRewardEvent
	if e.EventsConfig.RandomRewardEventConfig.Enabled {
		// Start of every day event interval
		rreIntv := time.Duration(e.EventsConfig.RandomRewardEventConfig.StepIntervalInSeconds) * time.Second
		rreTimer := time.NewTimer(rreIntv)
		log.Printf("[Events] Set random rewards event step interval to %v", rreIntv)
		EventsInfoLogger.Printf("[Events] Set random rewards event step interval to %v", rreIntv)

		go func() {
			for {
				select {
				case <-rreTimer.C:
					currMiners := Graviton_backend.GetAllMinerStats()

					// Retrieve from event storage the current details
					now := time.Now().UTC()
					year, month, day := now.Date()
					var todaysdate string
					// Date string for use in the 'key' of graviton store
					todaysdate = fmt.Sprintf("%v-%v-%v", strconv.Itoa(year), int(month), strconv.Itoa(day))

					// TODO: Probably better ways to accomplish this, however it's base-logical level to go about it for now
					// Determine pieces of the event start day
					eventStart := e.EventsConfig.RandomRewardEventConfig.StartDay
					eventStartSplit := strings.Split(eventStart, "-")
					eventStartYear := eventStartSplit[0]
					eventStartMonth := eventStartSplit[1]
					eventStartMonthInt, _ := strconv.Atoi(eventStartMonth)
					eventStartDay, _ := strconv.Atoi(eventStartSplit[2])

					// Determine pieces of the event end day
					eventEnd := e.EventsConfig.RandomRewardEventConfig.EndDay
					eventEndSplit := strings.Split(eventEnd, "-")
					eventEndYear := eventEndSplit[0]
					eventEndMonth := eventEndSplit[1]
					eventEndMonthInt, _ := strconv.Atoi(eventEndMonth)
					eventEndDay, _ := strconv.Atoi(eventEndSplit[2])

					// Determine pieces of the bonus event day, if it exists
					bonusEventStart := e.EventsConfig.RandomRewardEventConfig.Bonus1hrDayEventDate
					var bonusEventStartSplit []string
					var bonusEventStartYear, bonusEventStartMonth string
					var bonusEventStartMonthInt, bonusEventStartDay int
					if bonusEventStart != "" {
						bonusEventStartSplit = strings.Split(bonusEventStart, "-")
						bonusEventStartYear = bonusEventStartSplit[0]
						bonusEventStartMonth = bonusEventStartSplit[1]
						bonusEventStartMonthInt, _ = strconv.Atoi(bonusEventStartMonth)
						bonusEventStartDay, _ = strconv.Atoi(bonusEventStartSplit[2])
					}

					// Logical boundary between event start date and end date to ensure that "today" is between those areas, so that data is only logged when you're in an event window
					var inEventWindow bool
					var bonusEventInWindow bool

					log.Printf("[Events] Checking the year. Year now: %v , eventStartYear: %v, eventEndYear: %v", strconv.Itoa(year), eventStartYear, eventEndYear)
					if strconv.Itoa(year) == eventStartYear || strconv.Itoa(year) == eventEndYear {
						log.Printf("[Events] Checking the month. Month now: %v , eventStartMonth: %v, eventEndMonth: %v", int(month), eventStartMonthInt, eventEndMonthInt)
						if int(month) >= eventStartMonthInt && int(month) <= eventEndMonthInt {
							log.Printf("[Events] Checking the day. Day now: %v , eventStartDay: %v, eventEndDay: %v . day >= eventStartDay || day <= eventEndDay", strconv.Itoa(day), eventStartDay, eventEndDay)
							if eventStartMonthInt == eventEndMonthInt {
								if day >= eventStartDay && day <= eventEndDay {
									inEventWindow = true

									// Check if we are in a bonusEvent day
									if bonusEventStart != "" {
										if strconv.Itoa(year) == bonusEventStartYear {
											if int(month) == bonusEventStartMonthInt {
												if day == bonusEventStartDay {
													bonusEventInWindow = true
												}
											}
										}
									}
								} else {
									log.Printf("[Events] We are not in the event window.")
								}
							} else {
								if (day >= eventStartDay && int(month) >= eventStartMonthInt) || (day <= eventEndDay && int(month) <= eventEndMonthInt) {
									inEventWindow = true

									// Check if we are in a bonusEvent day
									if bonusEventStart != "" {
										if strconv.Itoa(year) == bonusEventStartYear {
											if int(month) == bonusEventStartMonthInt {
												if day == bonusEventStartDay {
													bonusEventInWindow = true
												}
											}
										}
									}
								} else {
									log.Printf("[Events] We are not in the event window.")
								}
							}

							// If we are in the event window, perform data storing and reward tasks.
							if inEventWindow {
								log.Printf("[Events] We are in the event window!!")
								log.Printf("[Events] Getting data for date: %v", todaysdate)
								storedstats := Graviton_backend.GetEventsData(todaysdate)

								// Compare and contrast and update the storage (as a whole)
								// This will take a look at all mining workers and determine earliest mining start date for an address and highest heartbeat for an address
								// This data will auto-reset when the next day is active
								if currMiners != nil {
									for _, cm := range currMiners {
										if storedstats != nil {
											//address := currMiner.Address
											address := cm.Address

											if storedstats[address] != nil {
												// Stats exist for this address, check and compare startedAt
												loadedStartedAt := storedstats[address].StartedAt
												if loadedStartedAt != 0 {
													// Already one, compare
													if cm.StartedAt < loadedStartedAt {
														// Set v.StartedAt
														log.Printf("[Events] Storing startedAt for %v . Current value: %v , New Value: %v (should be lower)", address, loadedStartedAt, cm.StartedAt)
														storedstats[address].StartedAt = cm.StartedAt
													} else {
														log.Printf("[Events] Stored startedAt for %v is already lowest, continue.", address)
													}
												} else {
													// Set v.StartedAt
													log.Printf("[Events] Stored startedAt value for %v is 0. New Value: %v", address, cm.StartedAt)
													storedstats[address].StartedAt = cm.StartedAt
												}

												// Stats exist for this address, check and compare lastBeat
												loadedLastBeat := storedstats[address].LastBeat
												if loadedLastBeat != 0 {
													// Already one, compare
													if cm.LastBeat > loadedLastBeat {
														// Set v.LastBeat
														log.Printf("[Events] Storing LastBeat for %v . Current value: %v , New Value: %v (should be lower)", address, loadedLastBeat, cm.LastBeat)
														storedstats[address].LastBeat = cm.LastBeat
													} else {
														log.Printf("[Events] Stored LastBeat for %v is already highest.", address)
													}
												} else {
													// Set v.LastBeat
													log.Printf("[Events] Stored LastBeat value for %v is 0. New Value: %v", address, cm.LastBeat)
													storedstats[address].LastBeat = cm.LastBeat
												}
											} else {
												// Store new stats
												tempMiner := &Miner{StartedAt: cm.StartedAt, LastBeat: cm.LastBeat}
												storedstats[address] = tempMiner
											}
										} else {
											// Store new stats
											storedstats = make(map[string]*Miner)
											address := cm.Address

											log.Printf("[Events] Storing details for address %v . Did not exist before.", address)
											storedAddressDetails := &Miner{StartedAt: cm.StartedAt, LastBeat: cm.LastBeat}

											storedstats[address] = storedAddressDetails
											log.Printf("[Events] Should be storing: %v", storedstats[address])
										}
									}

									// Now compare stored vs current and ensure there's no outliers like offline miner to online miner jumps etc. and if so, calculate the difference to the offset var
									// Checks will be performed to ensure that the startedAt is <= the beginning of the day within the final function to randomly reward. No reason to do this now.
									for k, v := range storedstats {
										address := k

										now := time.Now().UnixNano() / int64(time.Millisecond) / 1000

										// If last beat is not within the last 5 minutes
										log.Printf("[Events] Checking if lastbeat (%v) <= now (%v) - 300", v.LastBeat, now)
										todayStartWindow := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
										todaysStartTime := todayStartWindow.UnixNano() / int64(time.Millisecond) / 1000
										if v.LastBeat <= now-300 && v.LastBeat >= todaysStartTime {
											if storedstats[address].LastBeat != 0 && storedstats[address].StartedAt != 0 {
												storedstats[address].EventDataOffset += now - (v.LastBeat + storedstats[address].EventDataOffset + 300)
											} /*else if storedstats[address].LastBeat != 0 && storedstats[address].StartedAt != 0 {
												storedstats[address].EventDataOffset = now - (v.LastBeat + 300)
											}*/
											log.Printf("[Events] Address (%v) eventdataoffset %v", address, storedstats[address].EventDataOffset)
										}
									}

									writeWait, _ := time.ParseDuration("10ms")
									for Graviton_backend.Writing == 1 {
										time.Sleep(writeWait)
									}
									Graviton_backend.Writing = 1
									err := Graviton_backend.OverwriteEventsData(storedstats, todaysdate)
									Graviton_backend.Writing = 0
									if err != nil {
										log.Printf("[Events] Error overwriting events data")
									}
								} else {
									log.Printf("[Events] No stats to store for event, no connected miners.")
								}

								// Put addresses into a string slice
								now = time.Now().UTC()
								yesterday := now.AddDate(0, 0, -1)
								year, month, day = yesterday.Date()
								yesterdaysdate := fmt.Sprintf("%v-%v-%v", strconv.Itoa(year), int(month), strconv.Itoa(day))
								log.Printf("[Events] Yesterdays date: %v", yesterdaysdate)
								// Do not try to find a winner if today is the start day, need to have a full day of data first
								if todaysdate != e.EventsConfig.RandomRewardEventConfig.StartDay && len(storedstats) != 0 {
									// Check for existing payment processed - yes pendingpayment is confusing... just trust the structs/process <3
									yesterdayPayment := Graviton_backend.GetEventsPayment(yesterdaysdate)
									if yesterdayPayment == nil {
										backendStats := Graviton_backend.GetEventsData(yesterdaysdate)
										var tempMinerArr []string
										for k, _ := range backendStats {
											var mExist bool
											if backendStats[k].LastBeat == 0 {
												mExist = true
											}
											for _, m := range tempMinerArr {
												if k == m {
													mExist = true
												}
											}
											if !mExist {
												// Do logic to calculate the startedAt offset for the day
												yesterdayStartWindow := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
												unixStartTime := yesterdayStartWindow.UnixNano() / int64(time.Millisecond) / 1000
												log.Printf("YesterdayStartWindow: %v , %v", yesterdayStartWindow, unixStartTime)

												log.Printf("backendStats[k].StartedAt(%v) > unixStartTime (%v)", backendStats[k].StartedAt, unixStartTime)
												log.Printf("LastBeat at (%v) < unixStartTime (%v).", backendStats[k].LastBeat, unixStartTime)
												if backendStats[k].StartedAt > unixStartTime {
													// Do logic to calc difference
													log.Printf("StartedAt (%v) is greater than unixStartTime (%v). Difference adding to offset: %v", backendStats[k].StartedAt, unixStartTime, backendStats[k].StartedAt-unixStartTime)
													backendStats[k].EventDataOffset += backendStats[k].StartedAt - unixStartTime
												} else if backendStats[k].LastBeat < unixStartTime {
													log.Printf("LastBeat at (%v) is greater than unixStartTime (%v). Adding time.now - unixStartTime to offset", backendStats[k].LastBeat, unixStartTime)
													beatNow := time.Now().UTC().UnixNano() / int64(time.Millisecond) / 1000
													log.Printf("now(%v) - beatNow (%v) - unixStartTime (%v)", time.Now().UTC(), beatNow, unixStartTime)
													backendStats[k].EventDataOffset += beatNow - unixStartTime
												}

												yesterdayEndWindow := time.Date(year, month, day, 23, 59, 59, 9999, time.UTC)
												unixEndTime := yesterdayEndWindow.UnixNano() / int64(time.Millisecond) / 1000
												log.Printf("YesterdayEndWindow: %v , %v", yesterdayEndWindow, unixEndTime)

												yesterdayTimeWindow := unixEndTime - unixStartTime
												yesterdayTimeWindowFloat := float64(yesterdayTimeWindow)
												minerOffset := yesterdayTimeWindow - backendStats[k].EventDataOffset
												log.Printf("yesterdayTimeWindow (%v) - backendStats[k].EventDataOffset (%v)", yesterdayTimeWindow, backendStats[k].EventDataOffset)
												minerOffsetFloat := float64(minerOffset)
												offsetPercent := minerOffsetFloat / yesterdayTimeWindowFloat
												log.Printf("Offsetpercent: minerOffsetFloat (%v) / yesterdayTimeWindowFloat (%v)", minerOffsetFloat, yesterdayTimeWindowFloat)
												if offsetPercent >= e.EventsConfig.RandomRewardEventConfig.MinerPercentCriteria {
													log.Printf("Adding miner. Meets criteria: %v", offsetPercent)
													tempMinerArr = append(tempMinerArr, k)
												} else {
													log.Printf("Not adding miner, they did not meet the mining percent criteria: %v", offsetPercent)
												}
											}
										}

										if tempMinerArr != nil {
											log.Printf("[Events] Choosing the winner for date: %v", yesterdaysdate)
											rand.Seed(time.Now().Unix())
											n := rand.Int() % len(tempMinerArr)
											log.Printf("[Events] Chosen address string: %v , int: %v", tempMinerArr[n], n)

											log.Printf("[Events] Rewarding: %v", uint64(e.EventsConfig.RandomRewardEventConfig.RewardValueInDERO*e.CoinUnits))
											info := &PaymentPending{}
											info.Address = tempMinerArr[n]
											info.Amount = uint64(e.EventsConfig.RandomRewardEventConfig.RewardValueInDERO * e.CoinUnits)
											info.Timestamp = util.MakeTimestamp() / 1000

											writeWait, _ := time.ParseDuration("10ms")

											for Graviton_backend.Writing == 1 {
												time.Sleep(writeWait)
											}
											Graviton_backend.Writing = 1
											infoErr := Graviton_backend.WritePendingPayments(info)
											Graviton_backend.Writing = 0
											if infoErr != nil {
												log.Printf("[Events] Graviton DB err: %v", infoErr)
												EventsErrorLogger.Printf("[Events] Graviton DB err: %v", infoErr)
											}

											for Graviton_backend.Writing == 1 {
												time.Sleep(writeWait)
											}
											Graviton_backend.Writing = 1
											eventPaymentErr := Graviton_backend.WriteEventsPayment(info, yesterdaysdate)
											Graviton_backend.Writing = 0
											if eventPaymentErr != nil {
												log.Printf("[Events] Graviton DB err: %v", eventPaymentErr)
												EventsErrorLogger.Printf("[Events] Graviton DB err: %v", eventPaymentErr)
											}
										} else {
											log.Printf("[Events] No miners within yesterday's data. No rewards processed.")
										}
									} else {
										log.Printf("[Events] Payment was already processed for yesterday (%v). No rewards processed.", yesterdayPayment)
									}
								}
							}
						} else {
							log.Printf("[Events] We are not in the event window.")
						}
					} else {
						log.Printf("[Events] We are not in the event window.")
					}

					// If todaysdate is same as eventEnd + 1, payout eventEndDay. This will only be caught once and at the end of an event, since above logic will not catch the end date's reward
					// Put addresses into a string slice
					now = time.Now().UTC()
					yesterday := now.AddDate(0, 0, -1)
					year, month, day = yesterday.Date()
					yesterdaysdate := fmt.Sprintf("%v-%v-%v", strconv.Itoa(year), int(month), strconv.Itoa(day))
					log.Printf("[Events] Yesterdays date (end): %v", yesterdaysdate)
					if yesterdaysdate == e.EventsConfig.RandomRewardEventConfig.EndDay {
						// Check for existing payment processed - yes pendingpayment is confusing... just trust the structs/process <3
						yesterdayPayment := Graviton_backend.GetEventsPayment(yesterdaysdate)
						if yesterdayPayment == nil {
							backendStats := Graviton_backend.GetEventsData(yesterdaysdate)
							var tempMinerArr []string
							for k, _ := range backendStats {
								var mExist bool
								if backendStats[k].LastBeat == 0 {
									mExist = true
								}
								for _, m := range tempMinerArr {
									if k == m {
										mExist = true
									}
								}
								if !mExist {
									// Do logic to calculate the startedAt offset for the day
									yesterdayStartWindow := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
									unixStartTime := yesterdayStartWindow.UnixNano() / int64(time.Millisecond) / 1000
									log.Printf("YesterdayStartWindow: %v , %v", yesterdayStartWindow, unixStartTime)

									log.Printf("backendStats[k].StartedAt(%v) > unixStartTime (%v)", backendStats[k].StartedAt, unixStartTime)
									log.Printf("LastBeat at (%v) < unixStartTime (%v).", backendStats[k].LastBeat, unixStartTime)
									if backendStats[k].StartedAt > unixStartTime {
										// Do logic to calc difference
										log.Printf("StartedAt (%v) is greater than unixStartTime (%v). Difference adding to offset: %v", backendStats[k].StartedAt, unixStartTime, backendStats[k].StartedAt-unixStartTime)
										backendStats[k].EventDataOffset += backendStats[k].StartedAt - unixStartTime
									} else if backendStats[k].LastBeat < unixStartTime {
										log.Printf("LastBeat at (%v) is greater than unixStartTime (%v). Adding time.now - unixStartTime to offset", backendStats[k].LastBeat, unixStartTime)
										beatNow := time.Now().UTC().UnixNano() / int64(time.Millisecond) / 1000
										log.Printf("now(%v) - beatNow (%v) - unixStartTime (%v)", time.Now().UTC(), beatNow, unixStartTime)
										backendStats[k].EventDataOffset += beatNow - unixStartTime
									}

									yesterdayEndWindow := time.Date(year, month, day, 23, 59, 59, 9999, time.UTC)
									unixEndTime := yesterdayEndWindow.UnixNano() / int64(time.Millisecond) / 1000
									log.Printf("YesterdayEndWindow: %v , %v", yesterdayEndWindow, unixEndTime)

									yesterdayTimeWindow := unixEndTime - unixStartTime
									yesterdayTimeWindowFloat := float64(yesterdayTimeWindow)
									minerOffset := yesterdayTimeWindow - backendStats[k].EventDataOffset
									log.Printf("yesterdayTimeWindow (%v) - backendStats[k].EventDataOffset (%v)", yesterdayTimeWindow, backendStats[k].EventDataOffset)
									minerOffsetFloat := float64(minerOffset)
									offsetPercent := minerOffsetFloat / yesterdayTimeWindowFloat
									log.Printf("Offsetpercent: minerOffsetFloat (%v) / yesterdayTimeWindowFloat (%v)", minerOffsetFloat, yesterdayTimeWindowFloat)
									if offsetPercent >= e.EventsConfig.RandomRewardEventConfig.MinerPercentCriteria {
										log.Printf("[Events] Adding miner. Meets criteria: %v", offsetPercent)
										tempMinerArr = append(tempMinerArr, k)
									} else {
										log.Printf("[Events] Not adding miner (%v), they did not meet the mining percent criteria: %v", k, offsetPercent)
									}
								}
							}

							if tempMinerArr != nil {
								log.Printf("[Events] Choosing the winner for date: %v", yesterdaysdate)
								rand.Seed(time.Now().Unix())
								n := rand.Int() % len(tempMinerArr)
								log.Printf("[Events] Chosen address string: %v , int: %v", tempMinerArr[n], n)

								log.Printf("[Events] Rewarding: %v", uint64(e.EventsConfig.RandomRewardEventConfig.RewardValueInDERO*e.CoinUnits))
								info := &PaymentPending{}
								info.Address = tempMinerArr[n]
								info.Amount = uint64(e.EventsConfig.RandomRewardEventConfig.RewardValueInDERO * e.CoinUnits)
								info.Timestamp = util.MakeTimestamp() / 1000

								writeWait, _ := time.ParseDuration("10ms")

								for Graviton_backend.Writing == 1 {
									time.Sleep(writeWait)
								}
								Graviton_backend.Writing = 1
								infoErr := Graviton_backend.WritePendingPayments(info)
								Graviton_backend.Writing = 0
								if infoErr != nil {
									log.Printf("[Events] Graviton DB err: %v", infoErr)
									EventsErrorLogger.Printf("[Events] Graviton DB err: %v", infoErr)
								}

								for Graviton_backend.Writing == 1 {
									time.Sleep(writeWait)
								}
								Graviton_backend.Writing = 1
								eventPaymentErr := Graviton_backend.WriteEventsPayment(info, yesterdaysdate)
								Graviton_backend.Writing = 0
								if eventPaymentErr != nil {
									log.Printf("[Events] Graviton DB err: %v", eventPaymentErr)
									EventsErrorLogger.Printf("[Events] Graviton DB err: %v", eventPaymentErr)
								}
							} else {
								log.Printf("[Events] No miners within yesterday's data. No rewards processed.")
							}
						} else {
							log.Printf("[Events] Payment was already processed for yesterday (%v). No rewards processed.", yesterdayPayment)
						}
					}

					// Check if there's a 1hr event
					nowBonus := time.Now().UTC()
					log.Printf("Now Hour: %v", nowBonus.Hour())
					lastHourBonus := nowBonus.Add(-time.Hour * 1)
					log.Printf("lastHourBonus Hour: %v", lastHourBonus.Hour())
					year, month, day = lastHourBonus.Date()
					hour := lastHourBonus.Hour()
					lastHourBonusDate := fmt.Sprintf("%v-%v-%v-%v", strconv.Itoa(year), int(month), strconv.Itoa(day), hour)
					// If bonusEventStart is defined; bonusEventInWindow (current time is in the bonus window) and the day of the lastHourBonus.Date() is equal to the right day (meaning it works for the first and the last hour payouts)
					if bonusEventStart != "" && bonusEventInWindow == true && day == bonusEventStartDay {
						log.Printf("We are in bonus event window!")
						// Check for existing payment processed - yes pendingpayment is confusing... just trust the structs/process <3
						lastHourPayment := Graviton_backend.GetEventsPayment(lastHourBonusDate)
						if lastHourPayment == nil {
							backendStats := Graviton_backend.GetEventsData(todaysdate)
							var tempMinerArr []string
							for k, _ := range backendStats {
								var mExist bool
								if backendStats[k].LastBeat == 0 {
									mExist = true
								}
								for _, m := range tempMinerArr {
									if k == m {
										mExist = true
									}
								}
								if !mExist {
									// Do logic to calculate the startedAt offset for the day
									thisHourStartWindow := time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
									unixStartTime := thisHourStartWindow.UnixNano() / int64(time.Millisecond) / 1000
									log.Printf("thisHourStartWindow: %v , %v", thisHourStartWindow, unixStartTime)

									log.Printf("LastBeat at (%v) >= unixStartTime (%v).", backendStats[k].LastBeat, unixStartTime)
									if backendStats[k].LastBeat >= unixStartTime {
										log.Printf("[Events] Adding miner. LastBeat is within the hour.")
										tempMinerArr = append(tempMinerArr, k)
									} else {
										log.Printf("[Events] Not adding miner (%v), lastbeat is not within this hour", k)
									}
								}
							}

							if tempMinerArr != nil {
								log.Printf("[Events] Choosing the winner for this hour: %v", lastHourBonusDate)
								rand.Seed(time.Now().Unix())
								n := rand.Int() % len(tempMinerArr)
								log.Printf("[Events] Chosen address string: %v , int: %v", tempMinerArr[n], n)

								log.Printf("[Events] Rewarding: %v", uint64(e.EventsConfig.RandomRewardEventConfig.RewardValueInDERO*e.CoinUnits))
								info := &PaymentPending{}
								info.Address = tempMinerArr[n]
								info.Amount = uint64(e.EventsConfig.RandomRewardEventConfig.RewardValueInDERO * e.CoinUnits)
								info.Timestamp = util.MakeTimestamp() / 1000

								writeWait, _ := time.ParseDuration("10ms")

								for Graviton_backend.Writing == 1 {
									time.Sleep(writeWait)
								}
								Graviton_backend.Writing = 1
								infoErr := Graviton_backend.WritePendingPayments(info)
								Graviton_backend.Writing = 0
								if infoErr != nil {
									log.Printf("[Events] Graviton DB err: %v", infoErr)
									EventsErrorLogger.Printf("[Events] Graviton DB err: %v", infoErr)
								}

								for Graviton_backend.Writing == 1 {
									time.Sleep(writeWait)
								}
								Graviton_backend.Writing = 1
								eventPaymentErr := Graviton_backend.WriteEventsPayment(info, lastHourBonusDate)
								Graviton_backend.Writing = 0
								if eventPaymentErr != nil {
									log.Printf("[Events] Graviton DB err: %v", eventPaymentErr)
									EventsErrorLogger.Printf("[Events] Graviton DB err: %v", eventPaymentErr)
								}
							} else {
								log.Printf("[Events] No miners within this hour's data. No rewards processed.")
							}
						} else {
							log.Printf("[Events] Payment was already processed for this hour (%v). No rewards processed.", lastHourBonusDate)
						}
					}

					rreTimer.Reset(rreIntv)
				}
			}
		}()
	}
}

func logFileOutEvents(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/eventsError.log"
	} else {
		logFileName = "logs/events.log"
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
