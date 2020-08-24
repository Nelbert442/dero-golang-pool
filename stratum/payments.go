// Many payments integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"fmt"
	"log"
	"math/big"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
)

type PayoutsProcessor struct {
	config   *pool.PaymentsConfig
	backend  *RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

func NewPayoutsProcessor(cfg *pool.PaymentsConfig, s *StratumServer) *PayoutsProcessor {
	u := &PayoutsProcessor{config: cfg, backend: s.backend}
	u.rpc = s.rpc()
	return u
}

func (u *PayoutsProcessor) Start(s *StratumServer) {
	log.Println("Starting payouts")

	intv, _ := time.ParseDuration(u.config.Interval)
	timer := time.NewTimer(intv)
	log.Printf("Set payouts interval to %v", intv)

	payments := u.backend.GetPendingPayments()
	if len(payments) > 0 {
		log.Printf("Previous payout failed, you have to resolve it. List of failed payments:\n %v",
			formatPendingPayments(payments))
		return
	}

	locked, err := u.backend.IsPayoutsLocked()
	if err != nil {
		log.Println("Unable to start payouts:", err)
		return
	}
	if locked {
		log.Println("Unable to start payouts because they are locked")
		return
	}

	// Immediately process payouts after start
	u.process(s)
	timer.Reset(intv)

	go func() {
		for {
			select {
			case <-timer.C:
				u.process(s)
				timer.Reset(intv)
			}
		}
	}()
}

func (u *PayoutsProcessor) process(s *StratumServer) {
	if u.halt {
		log.Println("Payments suspended due to last critical error:", u.lastFail)
		return
	}
	mustPay := 0
	minersPaid := 0
	totalAmount := big.NewInt(0)
	payees, err := u.backend.GetPayees()
	if err != nil {
		log.Println("Error while retrieving payees from backend:", err)
		return
	}

	for _, login := range payees {
		amount, _ := u.backend.GetBalance(login)

		if !u.reachedThreshold(amount) {
			continue
		}
		mustPay++

		// Check if we have enough funds
		walletURL := fmt.Sprintf("http://%s:%v/json_rpc", u.config.WalletHost, u.config.WalletPort)
		poolBalanceObj, err := u.rpc.GetBalance(walletURL)
		if err != nil {
			u.halt = true
			u.lastFail = err
			break
		}
		poolBalance := poolBalanceObj.UnlockedBalance
		if poolBalance < amount {
			err := fmt.Errorf("Not enough balance for payment, need %v DERO, pool has %v DERO", amount, poolBalance)
			u.halt = true
			u.lastFail = err
			break
		}

		// Lock payments for current payout
		err = u.backend.LockPayouts(login, int64(amount))
		if err != nil {
			log.Printf("Failed to lock payment for %s: %v", login, err)
			u.halt = true
			u.lastFail = err
			break
		}
		log.Printf("Locked payment for %s, %v DERO", login, amount)

		// Debit miner's balance and update stats
		err = u.backend.UpdateBalance(login, int64(amount))
		if err != nil {
			log.Printf("Failed to update balance for %s, %v DERO: %v", login, int64(amount), err)
			u.halt = true
			u.lastFail = err
			break
		}

		// login can have ~workerId or +paymentID. These values will need to be trimmed off. Can potentially call the handler function to get the details
		// s.extractIDParts(login)
		/*value := hexutil.EncodeBig(amount)
		txHash, err := u.rpc.SendTransaction(u.config.Address, login, u.config.GasHex(), u.config.GasPriceHex(), value, u.config.AutoGas)
		if err != nil {
			log.Printf("Failed to send payment to %s, %v DERO: %v. Check outgoing tx for %s in block explorer and docs/PAYOUTS.md",
				login, amount, err, login)
			u.halt = true
			u.lastFail = err
			break
		}*/

		// Log transaction hash
		txHash := "test"
		err = u.backend.WritePayment(login, txHash, int64(amount))
		if err != nil {
			log.Printf("Failed to log payment data for %s, %v DERO, tx: %s: %v", login, int64(amount), txHash, err)
			u.halt = true
			u.lastFail = err
			break
		}

		minersPaid++
		totalAmount.Add(totalAmount, big.NewInt(int64(amount)))
		log.Printf("Paid %v DERO to %v, TxHash: %v", int64(amount), login, txHash)

		// Wait for TX confirmation before further payouts
		/*for {
			log.Printf("Waiting for tx confirmation: %v", txHash)
			time.Sleep(txCheckInterval)
			receipt, err := u.rpc.GetTxReceipt(txHash)
			if err != nil {
				log.Printf("Failed to get tx receipt for %v: %v", txHash, err)
				continue
			}
			// Tx has been mined
			if receipt != nil && receipt.Confirmed() {
				if receipt.Successful() {
					log.Printf("Payout tx successful for %s: %s", login, txHash)
				} else {
					log.Printf("Payout tx failed for %s: %s. Address contract throws on incoming tx.", login, txHash)
				}
				break
			}
		}*/
	}

	if mustPay > 0 {
		log.Printf("Paid total %v DERO to %v of %v payees", totalAmount, minersPaid, mustPay)
	} else {
		log.Println("No payees that have reached payout threshold")
	}

	// Save redis state to disk
	if minersPaid > 0 && u.config.BgSave {
		u.bgSave()
	}
}

func formatPendingPayments(list []*PendingPayment) string {
	var s string
	for _, v := range list {
		s += fmt.Sprintf("\tAddress: %s, Amount: %v DERO, %v\n", v.Address, v.Amount, time.Unix(v.Timestamp, 0))
	}
	return s
}

func (self PayoutsProcessor) reachedThreshold(amount uint64) bool {
	return self.config.Threshold < amount
}

func (self PayoutsProcessor) bgSave() {
	result, err := self.backend.BgSave()
	if err != nil {
		log.Println("Failed to perform BGSAVE on backend:", err)
		return
	}
	log.Println("Saving backend state to disk:", result)
}

func (self PayoutsProcessor) resolvePayouts() {
	payments := self.backend.GetPendingPayments()

	if len(payments) > 0 {
		log.Printf("Will credit back following balances:\n%s", formatPendingPayments(payments))

		for _, v := range payments {
			err := self.backend.RollbackBalance(v.Address, v.Amount)
			if err != nil {
				log.Printf("Failed to credit %v DERO back to %s, error is: %v", v.Amount, v.Address, err)
				return
			}
			log.Printf("Credited %v DERO back to %s", v.Amount, v.Address)
		}
		err := self.backend.UnlockPayouts()
		if err != nil {
			log.Println("Failed to unlock payouts:", err)
			return
		}
	} else {
		log.Println("No pending payments to resolve")
	}

	if self.config.BgSave {
		self.bgSave()
	}
	log.Println("Payouts unlocked")
}
