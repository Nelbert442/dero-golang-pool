// Many payments integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"fmt"
	"log"
	"math/big"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
	"github.com/deroproject/derosuite/globals"
	"github.com/deroproject/derosuite/walletapi"
)

type PayoutsProcessor struct {
	config   *pool.PaymentsConfig
	backend  *RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

/* Used when integrating with derosuite functions, currently not being used but in place incase functions are used later
type Transfer struct {
	rAddress	*address.Address
	PaymentID	[]byte
	Amount		uint64
	Fees		uint64
	TX			*transaction.Transaction
	TXID		crypto.Hash
	Size		float32
	Status		string
	Inputs		[]uint64
	InputSum	uint64
	Change		uint64
	Relay		bool
	OfflineTX	bool
	Filename	string
}
*/

var wallet *walletapi.Wallet

func NewPayoutsProcessor(cfg *pool.PaymentsConfig, s *StratumServer) *PayoutsProcessor {
	u := &PayoutsProcessor{config: cfg, backend: s.backend}
	u.rpc = s.rpc()
	return u
}

func (u *PayoutsProcessor) Start(s *StratumServer) {
	log.Println("Starting payouts")

	// Unlock payments when starting the process. This will re-attempt any potentially locked payments if errored out before unlocking
	err := u.backend.UnlockPayouts()
	if err != nil {
		log.Printf("Failed to unlock payment")
		//u.halt = true
		//u.lastFail = err
	}
	log.Printf("Unlocked payment")

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
	/*if u.halt {
		log.Println("Payments suspended due to last critical error:", u.lastFail)
		return
	}*/
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
		//log.Printf("Amount: %v, login: %v", amount, login)
		if !u.reachedThreshold(amount) {
			continue
		}
		mustPay++

		// Check if we have enough funds
		walletURL := fmt.Sprintf("http://%s:%v/json_rpc", u.config.WalletHost, u.config.WalletPort)
		poolBalanceObj, err := u.rpc.GetBalance(walletURL)
		if err != nil {
			// TODO: mark sick maybe for tracking and frontend reporting?
			log.Printf("Error when getting balance from wallet %s. Will try again in %s", walletURL, u.config.Interval)
			//u.halt = true
			//u.lastFail = err
			break
		}
		poolBalance := poolBalanceObj.UnlockedBalance
		if poolBalance < amount {
			log.Printf("Not enough balance for payment, need %v DERO, pool has %v DERO", amount, poolBalance)
			//u.halt = true
			//u.lastFail = err
			break
		}

		// Lock payments for current payout
		err = u.backend.LockPayouts(login, int64(amount))
		if err != nil {
			log.Printf("Failed to lock payment for %s: %v", login, err)
			//u.halt = true
			//u.lastFail = err
			break
		}
		log.Printf("Locked payment for %s, %v DERO", login, amount)

		// Address validations
		// We already do this for when the miner connects, we need to get those details/vars or just regen them as well as re-validate JUST TO BE SURE prior to attempting to send
		// NOTE: The issue with grabbing from the miners arr (s.miners), is that if they're not actively mining but get rewards from a past round, the query will not return their detail for payout

		address, workID, paymentID, fixDiff, isSolo := s.splitLoginString(login)
		_, _, _ = workID, fixDiff, isSolo

		// Validate Address
		validatedAddress, err := globals.ParseValidateAddress(address)
		_ = validatedAddress

		if err != nil {
			log.Printf("Invalid address format. Will not process payments - %v", address)
			break
		}

		// Send DERO - Native, TODO)
		// Issues with mutex and also running wallet process locally (with --rpc-server), as it can't get a lock to generate the transaction.
		// Problem comes in, then it can't use the daemon/wallet to do the work it needs and errors, since we're not providing authentication in config etc.
		// Future may be allow for auth to rpc-server within the config.json or other means as an option to run it that way. Otherwise will continue using the rpc option as the future option.
		/*
			// Validate Address
			wallet.SetDaemonAddress("http://127.0.0.1:30306")

			transfer.rAddress, err = globals.ParseValidateAddress(login)
			if err != nil {
				log.Printf("Invalid address format - %v", login)
				break
			} else {
				log.Printf("Valid Address")
			}

			transfer.PaymentID = nil

			// This fails out w/ mutex error, not sure TODO
			//curBalance, _ := wallet.Get_Balance()

			transfer.Amount = amount
			addr_list := []address.Address{*transfer.rAddress}
			amount_list := []uint64{transfer.Amount}
			fees_per_kb := uint64(0) // fees  must be calculated by walletapi

			tx, inputs, input_sum, change, err := wallet.Transfer(addr_list, amount_list, 0, hex.EncodeToString(transfer.PaymentID), fees_per_kb, 0)
			_ = inputs

			if err != nil {
				log.Printf("Error while building transaction: %s", err)
				break
			}

			transfer.OfflineTX = false

			transfer.Relay = build_relay_transaction(tx, inputs, input_sum, change, err, transfer.OfflineTX, amount_list)

			if !transfer.Relay {
				log.Printf("Error:  Unable to build the transfer.")
				break
			}

			err = wallet.SendTransaction(transfer.TX) // relay tx to daemon/network

			if err == nil {
				transfer.Status = "Success"
				transfer.TXID = transfer.TX.GetHash()
			} else {
				transfer.Status = "Failed"
				transfer.TXID = transfer.TX.GetHash()
				log.Printf("Error relaying transaction: %s", err)
				break
			}
			txHash := transfer.TXID.String()
		*/

		// Send DERO - RPC (working)
		var currPayout rpc.Transfer_Params
		currPayout.Mixin = u.config.Mixin
		currPayout.Unlock_time = 0
		currPayout.Get_tx_key = true
		currPayout.Do_not_relay = false
		currPayout.Get_tx_hex = true
		currPayout.Payment_ID = paymentID

		currPayout.Destinations = []rpc.Destinations{
			rpc.Destinations{
				Amount:  amount,
				Address: address,
			},
		}

		paymentOutput, err := u.rpc.SendTransaction(walletURL, currPayout)

		if err != nil {
			log.Printf("Error with transaction: %v", err)
			// Unlock payments for current payout
			err = u.backend.UnlockPayouts()
			if err != nil {
				log.Printf("Failed to unlock payment for %s: %v", login, err)
				//u.halt = true
				//u.lastFail = err
				break
			}
			log.Printf("Unlocked payment for %s, %v DERO", login, amount)
			break
		}
		log.Printf("Success: %v", paymentOutput)
		// Log transaction hash
		// TODO: Possibly better way to handle the []string returned for Tx_hash_list and usage below for redis stores. Maybe a rune split approach will be necessary (especially if in future multiple tx in one return)
		txHash := paymentOutput.Tx_hash_list
		txFee := paymentOutput.Fee_list
		// As pool owner, you probably want to store keys so that you can prove a send if required.
		txKey := paymentOutput.Tx_key_list

		if txHash == nil {
			log.Printf("Failed to generate transaction. It was sent successfully to rpc server, but no reply back.")

			// Unlock payments for current payout
			err = u.backend.UnlockPayouts()
			if err != nil {
				log.Printf("Failed to unlock payment for %s: %v", login, err)
				//u.halt = true
				//u.lastFail = err
				break
			}
			log.Printf("Unlocked payment for %s, %v DERO", login, amount)

			break
		}

		// Debit miner's balance and update stats
		err = u.backend.UpdateBalance(login, int64(amount))
		if err != nil {
			log.Printf("Failed to update balance for %s, %v DERO: %v", login, int64(amount), err)
			//u.halt = true
			//u.lastFail = err
			break
		}

		// Update stats for pool payments
		err = u.backend.WritePayment(login, txHash[0], txKey[0], txFee[0], u.config.Mixin, int64(amount))
		if err != nil {
			log.Printf("Failed to log payment data for %s, %v DERO, tx: %s, fee: %v, txKey: %v, Mixin: %v, error: %v", login, int64(amount), txHash[0], txFee[0], txKey[0], u.config.Mixin, err)
			//u.halt = true
			//u.lastFail = err
			break
		}

		minersPaid++
		totalAmount.Add(totalAmount, big.NewInt(int64(amount)))
		log.Printf("Paid %v DERO to %v, TxHash: %v, Fee: %v, Mixin: %v", int64(amount), login, txHash[0], txFee[0], u.config.Mixin)

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

/*
// handles the output after building tx, takes feedback, confirms or relays tx
func build_relay_transaction(tx *transaction.Transaction, inputs []uint64, input_sum uint64, change uint64, err error, offline_tx bool, amount_list []uint64) bool {

	if err != nil {
		log.Printf("Error building transaction: %s", err)
		return false
	}

	transfer.Inputs = append(transfer.Inputs, uint64(len(inputs)))
	transfer.InputSum = input_sum
	transfer.Change = change
	transfer.Size = float32(len(tx.Serialize())) / 1024.0
	transfer.Fees = tx.RctSignature.Get_TX_Fee()
	transfer.TX = tx

	amount := uint64(0)

	for i := range amount_list {
		amount += amount_list[i]
	}

	if input_sum != (amount + change + tx.RctSignature.Get_TX_Fee()) {
		return false
	}

	return true
}
*/

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

/*
// Unused atm
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
*/
