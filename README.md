# dero-golang-pool
Golang Mining Pool for DERO

#### Requirements
* Coin daemon (find the coin's repo and build latest version from source)
    * [Derosuite](https://github.com/deroproject/derosuite/releases/latest)
* [Golang](https://golang.org/dl/)
    * All code built and tested with Go v1.13.6
* [Redis](https://redis.io/)
    * [**Redis warning**](http://redis.io/topics/security): It's a good idea to learn about and understand software that you are using - a good place to start with redis is [data persistence](http://redis.io/topics/persistence).

**Do not run the pool as root** : create a new user without ssh access to avoid security issues :
```bash
sudo adduser --disabled-password --disabled-login your-user
```
To login with this user : 
```
sudo su - your-user
```

#### 1) Downloading & Installing

```bash
go get git.dero.io/Nelbert442/dero-golang-pool
```

#### 2) Configuration

Copy the `config_example.json` file of your choice to `config.json` then overview each options and change any to match your preferred setup.

Explanation for each field:
```javascript
{
    /*  Mining pool address */
	"address": "dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr",

    /*  True: Do not worry about verifying miner shares [faster processing, but potentially wrong algo], False: Validate miner shares with built-in derosuite functions */
	"bypassShareValidation": false,

    /*  Number of threads to spawn stratum */
	"threads": 1,

    /*  Defines algorithm used by pool. References to this switch are in miner.go */
	"algo": "astrobwt",

	/* 	Defines coin name, used in redis stores etc. */
	"coin": "DERO",

	/* Used for defining how many validated shares to submit in a row before passThru hashing [trusted] */
	"trustedSharesCount": 30,

    /*  Defines how often the upstream (daemon) getblocktemplate is refreshed.
        DERO blockchain is fast and runs on 27 Seconds blocktime. Best practice is to update your mining job at-least every second. 
        Bitcoin pool also updates miner job every 10 seconds and BTC blocktime is 10 mins -Captain [03/08/2020] .
        Example of 10 second updates for 10 minute blocktimes on BTC. ~10/600 * 27 = 0.45 */
	"blockRefreshInterval": "450ms",

	"hashrateExpiration": "3h",		// TTL for workers stats, usually should be equal to large hashrate window from API section

	"upstreamCheckInterval": "5s",  // How often to poll upstream (daemon) for successful connections

	/*
		List of daemon nodes to poll for new jobs. Pool will get work from the first one alive and
		check in the background for failed daemons to have as backup. Current block template of the pool
		is always cached in RAM, so even if daemons are switched, the block template remains (unless new block/work) 
	*/
	"upstream": [
		{
			"enabled": true,        // Set daemon enabled to true, utilized, or false, not utilized
			"name": "Derod",        // Set name for daemon connection
			"host": "127.0.0.1",    // Set address to reach daemon
			"port": 30306,          // Set port to append to host
			"timeout": "10s"        // Set timeout value of daemon connections
		},
		{
			"enabled": false,
			"name": "Remote Derod",
			"host": "derodaemon.nelbert442.com",
			"port": 20206,
			"timeout": "10s"
		}
	],

	"stratum": {
		"paymentId": {
			"addressSeparator": "+",	// Defines separator used from miner login to parse paymentID
		},
		"fixedDiff": {
			"addressSeparator": "."		// Defines separator used from miner login to parse fixed difficulty
		},
		"workerID": {
			"addressSeparator": "@"		// Defines separator used from miner login to parse workerID
		},

		"timeout": "15m",           // See SetDeadline - https://golang.org/pkg/net/

		"listen": [
			{
				"host": "0.0.0.0",  // Bind address
				"port": 1111,       // Port for mining apps to connect to
				"diff": 1000,       // Difficulty miners are set to on this port. TODO: varDiff and set diff to be starting diff
				"maxConn": 32768    // Maximum connections on this port
			},
			{
				"host": "0.0.0.0",
				"port": 3333,
				"diff": 3000,
				"maxConn": 32768
			},
			{
				"host": "0.0.0.0",
				"port": 5555,
				"diff": 5000,
				"maxConn": 32768
			}
		]
	},

	"api": {
		"enabled": true,				// Set api enabled to true, self-hosted api, or false, not hosted
		"purgeOnly": false,				// Set api to purgeOnly mode which will just call purge functions and not collect stats
		"purgeInterval": "10m",			// Set purge interval (for both purgeOnly and normal stats collections) of stale stats
		"listen": "0.0.0.0:8082",		// Set bind address and port for api [Note: poolAddr/api/* (stats, blocks, etc. defined in api.go)]
		"statsCollectInterval": "5s",	// Set interval for stats collection to run
		"hashrateWindow": "10m",		// Fast hashrate estimation window for each miner from its' shares
		"hashrateLargeWindow": "3h",	// Long and precise hashrate from shares
		"luckWindow": [64, 128, 256],	// Collect stats for shares/diff ratio for this number of blocks
		"payments": 30,					// Max number of payments to display in frontend
		"blocks": 50					// Max number of blocks to display in frontend
	},

	"redis": {
		"enabled": true,            // Set redis enabled to true, utilized, or false, not utilized
		"host": "127.0.0.1",    	// Set address to reach redis db over
		"port": 6379,               // Set port to append to host
		"password": "",             // Set password for db access
		"DB": 0                     // Set index of db
	},

	"unlocker": {
		"enabled": true,			// Set block unlocker enabled to true, utilized, or false, not utilized
		"poolFee": 0,				// Set pool fee. This will be taken away from the block reward (paid to the pool addr)
		"depth": 60,				// Set depth for block unlocks. This value is compared against the core base block depth for validation
		"interval": "10m"			// Set interval to check for block unlocks. The faster you check, the more noisy/busy that process can get.
	},

	"payments": {
		"enabled": false,			// Set payments enabled to true, utilized, or false, not utilized
		"interval": "30s",			// Run payments in this interval
		"mixin": 8,					// Define mixin for transactions
		"minPayment": 100,			// Define the minimum payment (uint64). i.e.: 1 DERO = 1000000000000
		"bgsave": true,				// Perform BGSAVE on Redis after successful payouts session
		"walletHost": "127.0.0.1",	// Defines the host of the wallet daemon
		"walletPort": "30309"		// Defines the port of the wallet daemon [DERO Mainnet defaults to 20209 and Testnet to 30309]
	}
}
```

#### 3) Build/Start the pool

Per-run basis:

```bash
go run main.go
```

Or build:

```bash
go build main.go
```

#### 4) Host the api

Once `config.json` has "api"."enabled" set to true, it will listen by default locally on :8082 (or whichever port defined). You can use an example below to pull the content, or just poll it directly in a browser:

Default API Stats (powershell):
```
(invoke-webrequest -uri 'http://127.0.0.1:8082/stats').Content | ConvertFrom-Json
```

Credits
---------

* [sammy007](https://github.com/sammy007) - Developer on [monero-stratum](https://github.com/sammy007/monero-stratum) project from which current project is forked.
* [JKKGBE](https://github.com/JKKGBE) - Developer on [open-zcash-pool](https://github.com/JKKGBE/open-zcash-pool) which is forked from [sammy007](https://github.com/sammy007) project [open-ethereum-pool](https://github.com/sammy007/open-ethereum-pool)