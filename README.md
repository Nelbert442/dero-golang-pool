# DERO Golang Pool
DERO Golang Pool is currently in alpha stages and feedback/fixes are always welcome in regards to this pool software. While many pools leverage Redis or other database flavors, I chose to follow-suit with DERO and leverage the newly released Graviton database, which was built and supported by the DERO development team. See [Backend database choices](#backend-database-choices) for more details regarding this choice. Please read through the README and code, I'd be more than happy to assist with fixing things via issues and other means, please feel free to reach out to me. My current 'live' pool using this codebase is [pool.dero.network](https://pool.dero.network).

## Pools Using this Codebase
* [pool.dero.network](https://pool.dero.network)
* [dero.xmrminers.club](https://dero.xmrminers.club/)
* [letshash.it/dero](https://letshash.it/dero)

## Features
* Developed in Golang
* Utilizing Graviton for backend, built and supported by deroproject core team
* In-built http/https server for web UI
* Mining hardware monitoring, track if workers are sick
* Keep track of accepts, rejects and block stats
* Daemon failover, leverage multiple daemons (upstreams) and pool will get work from the first alive node, while monitoring the rest for backups
* Concurrent shares processing by using multiple threads
* Supports mining rewards sent directly to an exchange or wallet
* Allows use of integrated addresses (dERi) and paymentIDs
* API (http/https) in JSON for easy integration to web frontend
* Utils functions and switch for mining algorithm support, this way you can modify which mining algo is required from config.json with ease and update code in only a couple places
* Support for fixed difficulty with minimum difficulty settings on a per-port basis
* Support for variable difficulty with maxjump flexibilities and customization settings
* Support of pool and solo mining
* PROP Payment Scheme
* Light-weight webpage with built-in basic pool statistics, but template used to get off the ground running.
* Allows for miner-set donations to some defined donation address. Simply add %5 (0-100, default is 0) to wallet address / username on connection and a percentage of each submitted share is donated.

### Future Features
* (FUTURE) PPLNS and potentially other pool schemes support
* (FUTURE) Management functions/go files for modifying/reporting of payments etc.
* (FUTURE) More feature sets within the frontend such as admin page etc.

## Table of Contents
1. [Requirements](#requirements) 
1. [Downloading & Installing](#downloading--installing) 
1. [Configuration](#configuration)
1. [Build/Start the pool](#buildstart-the-pool)
1. [Host the api](#host-the-api)
1. [Host the frontend](#host-the-frontend)
1. [SSL for API and frontend](#ssl-for-api-and-frontend)
1. [Backend database choices](#backend-database-choices)
1. [Donations](#donations)
1. [Credits](#credits)

### Requirements
* Coin daemon (find the coin's repo and build latest version from source)
    * [Derosuite](https://github.com/deroproject/derosuite/releases/latest)
* [Golang](https://golang.org/dl/)
    * All code built and tested with Go v1.13.6 on both Windows and Ubuntu 18.04

**Do not run the pool as root** : create a new user without ssh access to avoid security issues :
```bash
sudo adduser --disabled-password --disabled-login your-user
```
To login with this user : 
```
sudo su - your-user
```

### Downloading & Installing

* Supporting repos to get:
```bash
go get github.com/deroproject/derosuite/...
go get github.com/deroproject/graviton/...
go get github.com/gorilla/mux/...
```

* Get project repo:
```bash
go get github.com/Nelbert442/dero-golang-pool
```

### Configuration

Copy the `config_example.json` file of your choice to `config.json` then overview each options and change any to match your preferred setup.

Explanation for each field:
```javascript
{
    /*  Pool host that will be displayed on frontend for miners to connect to.
        IMPORTANT: This value is used to define the Graviton tree name for storage. Once you set this value, if you change it
		you will see data "loss" in the sense that you are now storing to a NEW tree and old data resides in an OLD tree.
		Once you hit gravitonMaxSnapshots, the TREE is migrated and all k/v pairs within and not the whole DB. TODO for future to move everything.
	*/
	"poolHost": "your_pool_host_name",

	/* Blockchain explorer, i.e. explorer.dero.io */
	"blockchainExplorer": "https://explorer.dero.io/block/{id}",

	/* Transaction explorer, i.e. explorer.dero.io */
	"transactionExplorer": "https://explorer.dero.io/tx/{id}",

    /*  Mining pool address */
	"address": "<pool_DERO_Address>",

	/*	Pool donation address - miners connecting with %<#> on login address will donate some % of shares to the defined donation address. Default value is 0 (no donation) */
	"donationAddress": "<pool_donation_DERO_Address>",

	/*	Pool donation description - used for describing where the donations go and displaying on frontend (#getting_started page) */
	"donationDescription": "Thank you for supporting our mining pool!",

    /*  True: Do not worry about verifying miner shares [faster processing, but potentially wrong algo], False: Validate miner shares with built-in derosuite functions */
	"bypassShareValidation": false,

    /*  Number of threads to spawn stratum */
	"threads": 1,

    /*  Defines algorithm used by pool. References to this switch are in miner.go */
	"algo": "astrobwt",

	/* 	Defines coin name */
	"coin": "DERO",

	/* Defines the base of DERO, 12 decimal places */
	"coinUnits": 1000000000000,

	/* Defines decimal places for frontend displaying */
	"coinDecimalPlaces": 4,

	/* Defines the difficulty target (in seconds) on average for a block to be found */
	"coinDifficultyTarget": 27,

	/* Used for defining how many validated shares to submit in a row before passThru hashing [trusted] */
	"trustedSharesCount": 30,

    /*  Defines how often the upstream (daemon) getblocktemplate is refreshed.
        DERO blockchain is fast and runs on 27 Seconds blocktime. Best practice is to update your mining job at-least every second. 
        Bitcoin pool also updates miner job every 10 seconds and BTC blocktime is 10 mins -Captain [03/08/2020] .
        Example of 10 second updates for 10 minute blocktimes on BTC. ~10/600 * 27 = 0.45 [You can go as low as you'd like, it just increases daemon queries]
	*/
	"blockRefreshInterval": "450ms",

	"hashrateExpiration": "3h",		// TTL for workers stats, usually should be equal to large hashrate window from API section. NOTE: Use "0s" for infinite expiration time

	"storeMinerStatsInterval": "5s",	// How often to run WriteMinerStats() to sync MinersMap and DB of all current miners. [Do not put this value in milliseconds, leave at least >= 1s, 2 is better]

	/*
		Defines how many snapshots (commits) are made to the live DB before migrating to a new DB. This value directly impacts the size of the DB growth over time.
		Pool has been tested to be working well into the 100k+ commit range, however DB size expanded well beyond 25-40GB when getting in the upper ranges thus the implementation of a migration
		When this value is hit, the pooldb directory will be renamed to pooldb_bak and a new pooldb directory will be provisioned. You have the pooldb_bak directory for any query or offloading to cold storage if required. This value is variable depending on the pool owner's comfort level and storage availability.
	*/
	"gravitonMaxSnapshots": 5000,

	/*
		Defines the length of time a process (read or write) will wait for the database migration to finish. In theory processes would not overlap, however as size grows and number of commits grows (since processed linearly), the migration time can increase past some preferred times and overlap with other actions. This protects those actions from pointing to an incorrect memory location for the DB as well as allows for the processes to complete efficiently and pause prior to starting anything extensive until the DB migration is completed.
		This value can be made as little or large as you'd like, preferably no larger than 250ms or so, since it'll just loop until it can't run, but probably shouldn't be lower than 25ms or so, so as not to spam processes (however shouldn't cause issues other than just larger than normal logs stating waiting on DB)
	*/
	"gravitonMigrateWait": "100ms",

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
		"donatePercent": {
			"addressSeparator": "%"		// Defines separator used from miner login to parse donation percentage (percentage of submitted shares that are donated to pool's donation address)
		},
		"soloMining": {
			"enabled": true,			// Defines whether or not solo mining is enabled. By setting this to false, even if a miner connects with the appropriate solo~ connection, their ID will not include solo
			"addressSeparator": "~"		// Defines separator used from miner login to parse soloMining
		},

		"timeout": "15m",           // See SetDeadline - https://golang.org/pkg/net/
		"healthCheck": true,		// Reply error to miner instead of job if redis isn't available (https://github.com/sammy007/monero-stratum)
		"maxFails": 100,			// Mark pool sick after this number of redis failures (https://github.com/sammy007/monero-stratum)

		"listen": [
			{
				"host": "0.0.0.0",  		// Bind address
				"port": 1111,       		// Port for mining apps to connect to
				"diff": 1000,       		// Difficulty miners are set to on this port. TODO: varDiff and set diff to be starting diff
				"minDiff": 500,				// Sets minimum difficulty that one can use for fixed (potentially for varDiff [future]) on a per-port basis
				"maxConn": 32768,    		// Maximum connections on this port
				"desc": "Low end hardware"	// Description of port configuration
			},
			{
				"host": "0.0.0.0",
				"port": 3333,
				"diff": 2500,
				"minDiff": 500,
				"maxConn": 32768,
				"desc": "Mid range hardware"
			},
			{
				"host": "0.0.0.0",
				"port": 5555,
				"diff": 5000,
				"minDiff": 500,
				"maxConn": 32768,
				"desc": "High end hardware"
			}
		],

		"varDiff": {
			"enabled": false,		// Set varDiff enabled to true, variable difficulty for non-fixed diff miners, or false, to default to above difficulty configurations or fixed difficulty
			"minDiff": 100,			// Set minimum difficulty for varDiff
			"maxDiff": 1000000,		// Set maximum difficulty for varDiff
			"targetTime": 20,		// Try to get 1 share per this many seconds
			"retargetTime": 120,	// Check to see if we should retarget every this many seconds
			"variancePercent": 30,	// Allow time to vary this % from target without retargetting
			"maxJump": 50			// Limit diff percent increase/decrease in a single retargetting
		}
	},

	"api": {
		"enabled": true,				// Set api enabled to true, self-hosted api, or false, not hosted
		"listen": "0.0.0.0:8082",		// Set bind address and port for api [Note: poolAddr/api/* (stats, blocks, etc. defined in api.go)]
		"statsCollectInterval": "5s",	// Set interval for stats collection to run
		"hashrateWindow": "10m",		// Fast hashrate estimation window for each miner from its' shares
		"payments": 30,					// Max number of payments to display in frontend
		"blocks": 50,					// Max number of blocks to display in frontend
		"ssl": false,					// Enable SSL for api
		"sslListen": "0.0.0.0:9092",	// Set bind address and port for SSL api
		"certFile": "fullchain.cer",	// Set full chain cert file. Includes cert, chain and ca. Located within same dir as exe file. TODO Future could use filepath package.
		"keyFile": "cert.key"			// Set key file for cert file. Located within same dir as exe file. TODO Future could use filepath package.
	},

	"unlocker": {
		"enabled": true,			// Set block unlocker enabled to true, utilized, or false, not utilized
		"poolFee": 0.1,				// Set pool fee. This will be taken away from the block reward (paid to the pool addr)
		"depth": 60,				// Set depth for block unlocks. This value is compared against the core base block depth for validation
		"interval": "5m"			// Set interval to check for block unlocks. The faster you check, the more noisy/busy that process can get.
	},

	"payments": {
		"enabled": false,			// Set payments enabled to true, utilized, or false, not utilized
		"interval": "10m",			// Run payments in this interval
		"mixin": 8,					// Define mixin for transactions
		"maxAddresses": 2,			// Define maximum number of addresses to send a single TX to [Usually safer to keep lower, but 1-5 should suffice]
		"minPayment": 100,			// Define the minimum payment (uint64). i.e.: 1 DERO = 1000000000000
		"walletHost": "127.0.0.1",	// Defines the host of the wallet daemon
		"walletPort": "30309"		// Defines the port of the wallet daemon [DERO Mainnet defaults to 20209 and Testnet to 30309]
	},

	"website": {
		"enabled": true,			// Set website enabled to true, utilized, or false, not utilized
		"port": "8080",				// Set the port for the website to be bound to
		"ssl": false,				// Enable SSL for website
		"sslPort": "9090",			// Set bind port for SSL site
		"certFile": "fullchain.cer",// Set full chain cert file. Includes cert, chain and ca. Located within same dir as exe file. TODO Future could use filepath package.
		"keyFile": "cert.key"		// Set key file for cert file. Located within same dir as exe file. TODO Future could use filepath package.
	},

	"poolcharts": {
		"updateInterval": "60s",	// Sets the update interval of 'pool' charts
		"hashrate": {
			"enabled": true,		// Sets storing of this chart data type to true/false
			"maximumPeriod": 86400	// Sets the maximum period of the chart on the frontend for displaying. This value is represented in seconds and should be equal or greater to / divisible by updateInterval
		},
		"miners": {
			"enabled": true,
			"maximumPeriod": 86400
		},
		"workers": {
			"enabled": true,
			"maximumPeriod": 86400
		},
		"difficulty": {
			"enabled": true,
			"maximumPeriod": 604800
		}
	}
}
```

### Build/Start the pool

Per-run basis:

```bash
go run main.go
```

Or build:

```bash
go build main.go
```

NOTE: logs/ and pooldb/ directories are created in the working directory. Keep this in mind if you are configuring systemd runs or when running the app itself.

If you intend to run with systemd, you can leverage similar configuration to below:

```
[Unit]
Description=dero-golang-pool
After=network.target

[Service]
Type=simple
Restart=on-failure
RestartSec=10
SyslogIdentifier=dero-golang-pool
ExecStart=/pathtoyourdir/yourexecutable
WorkingDirectory=/pathtoyourdir

[Install]
WantedBy=multi-user.target
```

### Host the api

Once `config.json` has "api"."enabled" set to true, it will listen by default locally on :8082 (or whichever port defined). You can use an example below to pull the content, or just poll it directly in a browser:

API Examples:

* ".../api/stats" Example:

```json
{"blocksTotal":18,"candidates":null,"candidatesTotal":0,"config":{"algo":"astrobwt","blockchainExplorer":"http://127.0.0.1:8081/block/{id}","coin":"DERO","coinDecimalPlaces":4,"coinDifficultyTarget":27,"coinUnits":1000000000000,"fixedDiffAddressSeparator":".","payIDAddressSeparator":"+","paymentInterval":30,"paymentMinimum":10000000000,"paymentMixin":8,"poolFee":0.1,"poolHost":"127.0.0.1","ports":[{"diff":1000,"minDiff":500,"host":"0.0.0.0","port":1111,"maxConn":32768},{"diff":2500,"minDiff":500,"host":"0.0.0.0","port":3333,"maxConn":32768},{"diff":5000,"minDiff":500,"host":"0.0.0.0","port":5555,"maxConn":32768}],"transactionExplorer":"http://127.0.0.1:8081/tx/{id}","unlockDepth":5,"unlockInterval":10,"version":"1.0.0","workIDAddressSeparator":"@"},"immature":[{"Hash":"770efbc1377ca0f1818ac9e01b0f697bd461e716160b24826b6b96931ac392d2","Address":"dEToUEe...8gVNr","Height":1017,"Orphan":false,"Timestamp":1600807603,"Difficulty":22254,"TotalShares":29975,"Reward":2351321493449,"Solo":false},{"Hash":"efca19034b80b48366f984a2bdb81647e786481a1528942d406412b219109f6a","Address":"dEToUEe...8gVNr","Height":1014,"Orphan":false,"Timestamp":1600807420,"Difficulty":21816,"TotalShares":2000,"Reward":2345322388119,"Solo":false},{"Hash":"c3d54ee8d3c7919e0f426ec964516efa33f5d00b4608536c47e389329677425d","Address":"dEToUEe...8gVNr","Height":1016,"Orphan":false,"Timestamp":1600807598,"Difficulty":22254,"TotalShares":27780,"Reward":2345321791672,"Solo":false},{"Hash":"5ba9184f441c125fd67549d1aeecc8a1d1d664d51e1caf62b0357089492a1ee3","Address":"dEToUEe...8gVNr","Height":1013,"Orphan":false,"Timestamp":1600807411,"Difficulty":21600,"TotalShares":2000,"Reward":2345322686342,"Solo":false},{"Hash":"1c3bfe247f02f44c60301bfa54f85fa7e18f1604320ee8f2a775dea66567d128","Address":"dEToUEe...8gVNr","Height":1015,"Orphan":false,"Timestamp":1600807439,"Difficulty":22034,"TotalShares":5000,"Reward":2349822089896,"Solo":false}],"immatureTotal":5,"lastblock":{"Difficulty":"22254","Height":1017,"Timestamp":1600807598,"Reward":2351321493449,"Hash":"770efbc1377ca0f1818ac9e01b0f697bd461e716160b24826b6b96931ac392d2"},"matured":[{"Hash":"339ad336c07e86913f388fb45fc3d03dc03ef9ae7cdd82e98e7ee0d97c470f79","Address":"dEToUEe...8gVNr","Height":1000,"Orphan":false,"Timestamp":1600806375,"Difficulty":21600,"TotalShares":13000,"Reward":2354326563247,"Solo":false},{"Hash":"b2cbf4b90d36a10521092ea3bd8d20d0a29676b190492bb715b188fec17b0130","Address":"dEToUEe...8gVNr","Height":1007,"Orphan":false,"Timestamp":1600807040,"Difficulty":21600,"TotalShares":0,"Reward":2349824475682,"Solo":false},{"Hash":"4454bf01932bc8ae601e8aee345a294e8fde99790e05b71a481b7c4eec4bd084","Address":"dEToUEe...8gVNr","Height":1008,"Orphan":false,"Timestamp":1600807153,"Difficulty":21600,"TotalShares":0,"Reward":2349824177459,"Solo":false},{"Hash":"aadf5246f36cc098b341bf6c694dd08d6ca6969b0784d91c82f3cb3791812652","Address":"dEToUEe...8gVNr","Height":1011,"Orphan":false,"Timestamp":1600807224,"Difficulty":21600,"TotalShares":12000,"Reward":2349823282789,"Solo":false},{"Hash":"dfa60fede87c7c4e7d351c54b87e46c3239209ae10d6db58050a27a9b147457d","Address":"dEToUEe...8gVNr","Height":1012,"Orphan":false,"Timestamp":1600807401,"Difficulty":21600,"TotalShares":5000,"Reward":2354322984565,"Solo":false},{"Hash":"a6eccb0be31558bed06a8add669fe7846d388410e09bb37e8a29c1d5ab992f3e","Address":"dEToUEe...8gVNr","Height":1003,"Orphan":false,"Timestamp":1600806585,"Difficulty":21600,"TotalShares":10500,"Reward":2345325668576,"Solo":false},{"Hash":"f79af5914e15373fa998819cfacc7d74ffe18bb315787572c7fbbe1bb93aaed4","Address":"dEToUEe...8gVNr","Height":1004,"Orphan":false,"Timestamp":1600806855,"Difficulty":21600,"TotalShares":43500,"Reward":2345325370353,"Solo":false},{"Hash":"da99e1f3600508708a38f48959210ca9de914ab524aaa153882fa04c3873811a","Address":"dEToUEe...8gVNr","Height":1010,"Orphan":false,"Timestamp":1600807222,"Difficulty":21600,"TotalShares":0,"Reward":2349823581012,"Solo":false},{"Hash":"3fe81b154a9f4a07fce72d621fbaf169e457baf918be8d092a9b735a2159ce73","Address":"dEToUEe...8gVNr","Height":1002,"Orphan":false,"Timestamp":1600806516,"Difficulty":21600,"TotalShares":11500,"Reward":2345325966800,"Solo":false},{"Hash":"1068ccc0d92c1d49d375a675018154c29b5404bbb297b0f2da329154efe9e832","Address":"dEToUEe...8gVNr","Height":1006,"Orphan":false,"Timestamp":1600807020,"Difficulty":21600,"TotalShares":11250,"Reward":2345324773905,"Solo":false},{"Hash":"98310319fd9e80d97742e4e906a8b594f5423122b6a133511c672aaedfa29277","Address":"dEToUEe...8gVNr","Height":1001,"Orphan":false,"Timestamp":1600806383,"Difficulty":21600,"TotalShares":0,"Reward":2345326265023,"Solo":false},{"Hash":"e5fbce21b8003876d249ff2b050c474c44bc54dbfc7069d1845100d6b55cae42","Address":"dEToUEe...8gVNr","Height":1009,"Orphan":false,"Timestamp":1600807188,"Difficulty":21600,"TotalShares":0,"Reward":2349823879235,"Solo":false},{"Hash":"38984e8ac3ccd2c1ebc4eba781d38a4ecc76d461c80731d6c81ad94265e9d8e4","Address":"dEToUEe...8gVNr","Height":1005,"Orphan":false,"Timestamp":1600806908,"Difficulty":21600,"TotalShares":13500,"Reward":2345325072129,"Solo":false}],"maturedTotal":13,"miners":[{"LastBeat":1600807678,"StartedAt":1600807391,"ValidShares":36,"InvalidShares":0,"StaleShares":0,"Accepts":6,"Rejects":0,"RoundShares":29975,"Hashrate":151,"Offline":false,"Id":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","Address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","IsSolo":false}],"now":1600807685,"payments":[{"Hash":"205e4ac6547a784eb94cba28f50f4a26595f3335ae28a8d3d39dccdf6e0fae10","Timestamp":1600807021,"Payees":1,"Mixin":8,"Amount":2345326265023},{"Hash":"88621a2fee06d0c2d97b8bf5137ed26d22789ec5602263bcad9505c32f9caaf1","Timestamp":1600807202,"Payees":1,"Mixin":8,"Amount":2342980044983},{"Hash":"c24bedcaa513204d5663028821559379544754132d515030c68cf75f76a9eb70","Timestamp":1600807263,"Payees":1,"Mixin":8,"Amount":2342979449131},{"Hash":"2616b795413d6207da75aff72c1b66fd17af3cb7f99fca06bd073c60bd398088","Timestamp":1600807627,"Payees":1,"Mixin":8,"Amount":4699442121086},{"Hash":"e64c7bed69b3dfd2aa02100e9790dfa3e4904c63f59bd5067e4d0f71dbbb4b19","Timestamp":1600806931,"Payees":1,"Mixin":8,"Amount":2351972236684},{"Hash":"186615582db0e54b2e21c23f715d82ccc8b686e3aaeb243486a805517def5872","Timestamp":1600807051,"Payees":1,"Mixin":8,"Amount":2342980640833},{"Hash":"969334e0cd6e40947d9d016509965c7e52ef66e17ed650e700d29285f9c6824d","Timestamp":1600807172,"Payees":1,"Mixin":8,"Amount":2342980342907},{"Hash":"c2f3413e0579de5bba9bd10e810586d051f7a4b4e37e1f316278f15daf5e52ca","Timestamp":1600807233,"Payees":1,"Mixin":8,"Amount":2342979747057},{"Hash":"3eaa0b54c80b7856b46226d927cf114a7abbcbeb8a947cb7d9769590c9abbc24","Timestamp":1600807417,"Payees":1,"Mixin":8,"Amount":2349824475682},{"Hash":"b4e24d9a16ab1a3ae7c9254f43e660b3e697330925d933601c289fecc75f1e8e","Timestamp":1600807447,"Payees":1,"Mixin":8,"Amount":4699647460247}],"poolHashrate":151,"soloHashrate":0,"totalMinersPaid":1,"totalPayments":10,"totalPoolMiners":1,"totalSoloMiners":0}
```

* ".../api/accounts?address=<yourwalletaddress>" Example:

```json
{"address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","miners":[{"LastBeat":1603719621,"StartedAt":1603719611,"ValidShares":3,"InvalidShares":0,"StaleShares":0,"Accepts":0,"Rejects":0,"LastRoundShares":0,"RoundShares":4000,"Hashrate":0,"Offline":true,"Id":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","Address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","IsSolo":false},{"LastBeat":1603719643,"StartedAt":1603719633,"ValidShares":1,"InvalidShares":0,"StaleShares":0,"Accepts":0,"Rejects":0,"LastRoundShares":0,"RoundShares":0,"Hashrate":0,"Offline":true,"Id":"solo~dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","Address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","IsSolo":true}],"payments":[{"Hash":"fae0a899fac54452f90bc4a0c883705fd3ebc17193d169345b3b0476ab5ab48f","Timestamp":1603719241,"Payees":1,"Mixin":8,"Amount":2344919251485},{"Hash":"54656d899b0764639302f19ff6a56985d939b51e3f5748325d04154fadc1ac83","Timestamp":1603719152,"Payees":1,"Mixin":8,"Amount":2344919549085},{"Hash":"7f6a32ab4d95b527cf0b6b3f9a5f4ce52ef2d136d3910459d60ae6a3ad943425","Timestamp":1603718732,"Payees":1,"Mixin":8,"Amount":2340424346685},{"Hash":"0a98cc001b1a677c31c6ac2747b41ba86722b43ef9118299760c8bf80e16cd55","Timestamp":1603718341,"Payees":1,"Mixin":8,"Amount":2350914144285},{"Hash":"10a9632e96d50584ed575e4176393ca30057299e17139fdb16ddb9b702a6c6f4","Timestamp":1603717846,"Payees":1,"Mixin":8,"Amount":2344920441886},{"Hash":"b88604f42dede0d2427c63cbc4bff7d908a36d3fffe2a4080c49d2482686b741","Timestamp":1603717696,"Payees":1,"Mixin":8,"Amount":2344920739487},{"Hash":"0997ecd4ba65e042ed8942769ca57c3facbeccad2ade681de19f780ec05e2843","Timestamp":1603717635,"Payees":1,"Mixin":8,"Amount":2344921037087},{"Hash":"485e602aa179abcc39e14afe1c41aeee5716ee5ccf0ab2a66be9027ed4e820f1","Timestamp":1603717125,"Payees":1,"Mixin":8,"Amount":2344921334688},],"poolHashrate":0,"soloHashrate":0,"totalPayments":196,"totalPoolMiners":0,"totalSoloMiners":0}
```

### Host the frontend

Once `config.json` has "website"."enabled" set to true, it will listen by default locally on :8080 (or whichever port defined). It will leverage standard js/html/css files that a static webpage would, and integrate with the API above in #4.

website.go is the runner, which just starts the listenandserve on the port defined, then serves up content within /website/pages , feel free to make modifications to folder structure, just be sure to update website.go

![DERO Pool Home](images/home.PNG?raw=true "DERO Pool Home") 
![DERO Pool Getting Started](images/gettingstarted.PNG?raw=true "DERO Pool Getting Started")
![DERO Pool Worker Stats](images/workerstats.PNG?raw=true "DERO Pool Worker Stats")
![DERO Pool Blocks](images/poolBlock.PNG?raw=true "DERO Pool Blocks")
![DERO Pool Pay](images/poolpayment.PNG?raw=true "DERO Pool Pay")

### SSL for API and frontend

Within `config.json`, there are SSL sections within both API and website to leverage in order to define your key/cert files and ports to run the website or api locally. The cert and key files are to be placed in the same directory as your built package. The cert file should include cert, chain and ca while the key file is your private key. Store these files locally and make sure to .gitignore them properly, by default there is *.key and *.cer , however different folks use different file types.

### Backend database choices

Due to the nature of this pool being developed with [DERO](https://github.com/deroproject/) in mind, I decided to follow-suit with configuring the pool to leverage the same newly-released [Graviton](https://github.com/deroproject/graviton) database as the backend for the pool. I'll go into some currently existing cons and workarounds I've implemented, however this pool could easily be re-configured for other DB types such as: redis, boltdb, badgerdb, Graviton etc. Redis was used in previous commit history, you can also see implementation of redis on other pools, see Credits section.

Some workarounds I've implemented to get Graviton to be sustainable are as follows:

* Graviton performs snapshots on each commit of data. This is meaning each time you write data to it, a new snapshot is made for version control history which can be re-referenced at any point in time and used to diff against current or two different snapshots / trees pretty much anytime you'd like.

In early adaptations, I realized that within a short period of time I was at 100,000+ commits which ballooned the DB size quite drastically. This, in part, was due to my heavy commit nature I initially implemented as well as a few other pieces that have been optimized a bit during time. While the 'point' is to retain this historical backup of snapshots, I just didn't need that requirement for my implementation. In order to not just scrap it and go back to boltdb or redis, I continued on and decided I'd retain X number of commit history and a single backup, in the event I ever wanted to push the backups to some cloud/cold storage and retain for time.

In order to do this, I define some gravitonMaxSnapshots that I check for upon every read/write of the DB (low ms check) until I reach the value (or exceed it) and then grab all the k/v pairs, perform a rename of the current pooldb directory to pooldb_bak, then provision a new pooldb store and put the k/v pairs into it and commit then continue on. During this time there is a g.migrating attribute (set to 0 (not migrating) or 1 (migrating)) which upon every read/write is checked against. If the db is migrating while some read/write action attempts to utilize it, the process will 'wait' for gravitonMigrateWait amount of time (say 100ms or so) and continuously loop through until the process is open. Since this happens at all reads and writes, there are no tested issues so far that have arose for processes to get stuck midway since the commits of processes occur at the tail end, rather than along the way.

Over time it may seem that Graviton is not the right fit, however I did not let that keep me away as I liked the functionality of it, portability of the directories (can copy/paste live data without corruption), and other potential future featuresets. To each their own, anyone is welcome who uses this repo to implement whichever form of DB they'd like. I thought at one point keeping a history so you could easily switch between using redis or graviton or other, however that seemed a bit too ambitious for alpha stages and maybe something down the line :)

### Donations

I have made it a long-standing tradition since I first started hosting mining pools for DERO (testnet and mainnet alike) that I would host them for free at a 0% fee operation as often/long as I personally can sustain it. We are a few years down the line, and I truly enjoy every minute of working on these pools and this release is the first time I can say that I know the ins & outs of the pool, rather than just utilizing another pool codebase and modifying it a bit to just work. With some assistance in [Credits](#credits) below for some feature sets and ideas, the groundwork was laid and heavy re-write/modification and formation took place into what this pool software is today. I hope that you can enjoy it as much as I have, and here's to advancements in technology wherever possible. Your interest is much appreciated and feedback is always welcome.

```
dERopdjpGmr2DEwQJdRrKc8M6obca9NQu2EaC2fNe3RNHonYcCfqmjGF7NBEHoB8dpLXWhnjdW7dugFTVhofuKTb4sfzmyBSAj
```

Credits
---------

* [sammy007](https://github.com/sammy007) - Developer on [monero-stratum](https://github.com/sammy007/monero-stratum) which started the basis for stratum building for this project.
* [JKKGBE](https://github.com/JKKGBE) - Developer on [open-zcash-pool](https://github.com/JKKGBE/open-zcash-pool) which is forked from [sammy007](https://github.com/sammy007) project [open-ethereum-pool](https://github.com/sammy007/open-ethereum-pool) for some additional ideas/thoughts throughout dev when REDIS was utilized, but later migrated to Graviton from scratch with other implementations.
* [Graviton](https://github.com/deroproject/graviton) - Graviton DB which is leveraged within this project for backend data storage.
* [Derosuite](https://github.com/deroproject/derosuite) - Derosuite (DERO) which is the cryptocurrency in which this pool was originally built for and focused for.