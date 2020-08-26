# dero-golang-pool
Golang Mining Pool for DERO

#### Features
* Developed in Golang
* In-built http server for web UI
* Mining hardware monitoring, track if workers are sick
* Keep track of accepts, rejects and block stats
* Daemon failover, leverage multiple daemons (upstreams) and pool will get work from the first alive node, while monitoring the rest for backups
* Concurrent shares processing by using multiple threads
* Supports mining rewards sent directly to an exchange or wallet
* Allows use of integrated addresses (dERi)
* API in JSON for easy integration to web frontend
* Utils functions and switch for mining algorithm support, this way you can modify which mining algo is required from config.json with ease and update code in only a couple places

##### Future Features
* (FUTURE) Support of pool and solo mining
* (FUTURE) User-friendly design for webpage
* (FUTURE) PROP/PPLNS and other pool schemes support
* (FUTURE) Support for custom fixed and variable difficulties

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
		"healthCheck": true,		// Reply error to miner instead of job if redis isn't available (https://github.com/sammy007/monero-stratum)
		"maxFails": 100,			// Mark pool sick after this number of redis failures (https://github.com/sammy007/monero-stratum)

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
	},

	"website": {
		"enabled": true,			// Set website enabled to true, utilized, or false, not utilized
		"port": "8080"				// Set the port for the website to be bound to
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

API Examples:

* ".../api/stats" Example:
```json
{"candidatesTotal":0,"hashrate":0,"immatureTotal":0,"maturedTotal":18,"minersTotal":0,"nodes":[{"difficulty":"21600","height":"304","lastBeat":"1598387614","name":"DERO"}],"now":1598388148,"stats":{"lastBlockFound":1598372847,"roundShares":1000}}
```

* ".../api/miners" Example:
```json
{"hashrate":58,"miners":{"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr":{"lastBeat":1598388251,"hr":58,"offline":false}},"minersTotal":1,"now":1598388256}
```

* ".../api/payments" Example:
```json
{"payments":[{"address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr+21e63470b34e45a7d6bc7f28fd95c22ecbcb9e98f42b23709f253c4ee2b232ef","amount":2234574584664,"timestamp":1598370885,"tx":"dc54781434adc31a3023cee9357c296ec2437852e55ced502aa751cb5a1a9bea"},{"address":"dETiVQuGunuXoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP7V8KdLKzLkj5B9dneLXW8","amount":117609188667,"timestamp":1598370057,"tx":"298b46754b8d8546399b853d16551b1705b84d54ee436e6d24b4bc08afe14985"},{"address":"dETiVQuGunuXoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP7V8KdLKzLkj5B9dneLXW8","amount":939674828513,"timestamp":1598369842,"tx":"7a39bba315e113e67d5627195e67bf7424ea91be2320f13cb58d78f43544c08e"},{"address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","amount":1409512242769,"timestamp":1598369842,"tx":"1fdba086065723207645e7533d5b4d08c8801a531526006c9bf53cc6c83aeaf5"},{"address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","amount":4690882536423,"timestamp":1598369586,"tx":"2a2dfb06183883c1548f7da85994ae576b0eef1791f6a451e777024b6adf1737"},{"address":"dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr","amount":2355181965140,"timestamp":1598369496,"tx":"74d6ff58a4d1b5ff3493aafd80c7b3bf2995205a25cdec1fd8a3c1fa5276743f"}],"paymentsTotal":6}
```

* ".../api/blocks" Example:
```json
{"candidates":null,"candidatesTotal":0,"immature":null,"immatureTotal":0,"luck":{"18":{"luck":0.9689881025139554,"orphanRate":0}},"matured":[{"height":303,"timestamp":1598372847,"difficulty":21600,"shares":9000,"orphan":false,"hash":"e6074915fecbaa55eda9616f1053234d23724af284eb87f7ab7b8531f7f16c97","reward":"2345534434387"},{"height":302,"timestamp":1598372806,"difficulty":21816,"shares":26000,"orphan":false,"hash":"17419fb7b7dcc9755d6634849895da5abd35fe517cd8bef241e27be9980dd9c3","reward":"2345534732637"},{"height":301,"timestamp":1598372625,"difficulty":21600,"shares":70000,"orphan":false,"hash":"b76557cb3a0a9d53aedf8ef1e00d39cf5f90862a3c68f1164563a0ab61fd3404","reward":"2345535030887"},{"height":300,"timestamp":1598372196,"difficulty":21600,"shares":42000,"orphan":false,"hash":"b275dc380c8f599c424ba65605b6592e84b473f59f59c3d667cc223e8cef34d9","reward":"2345535329137"},{"height":299,"timestamp":1598371955,"difficulty":21600,"shares":9000,"orphan":false,"hash":"25c399cc0844ef31478308ef9eed77c376a3d031f4fb8e83d8a0f12b20f243e7","reward":"2345535627388"},{"height":298,"timestamp":1598371910,"difficulty":21600,"shares":5000,"orphan":false,"hash":"397d3a96e4ef45a7276a8bb387f678bd6d1bc34c4dd80810dd7848535c83ab9a","reward":"2345535925638"},{"height":297,"timestamp":1598371891,"difficulty":21600,"shares":5000,"orphan":false,"hash":"31e601c44de50056d0f79e30a2b2a07693d874e7c1ba94a4974d5da846abb374","reward":"2345536223888"},{"height":296,"timestamp":1598371871,"difficulty":21600,"shares":16000,"orphan":false,"hash":"cacadef57e58a1b5c801463b13345064885e95534fdfcab69e2a475210337905","reward":"2345536522139"},{"height":295,"timestamp":1598371814,"difficulty":21600,"shares":16000,"orphan":false,"hash":"af6a99396e9ca606ae6f579fc419cbdc7dbb167534a4b8e9996567494787737d","reward":"2345536820389"},{"height":294,"timestamp":1598371770,"difficulty":21600,"shares":7000,"orphan":false,"hash":"b45865df9c7988004890b759e2e440f57468b20bfb9fc509fcadf96bb0f8d16f","reward":"2345537118639"},{"height":293,"timestamp":1598371733,"difficulty":21600,"shares":18000,"orphan":false,"hash":"1c4d4f3861a08e68c9abab84021e23d2773debb8d8e4f5c8326e4082942910ba","reward":"2345537416890"},{"height":292,"timestamp":1598371637,"difficulty":21600,"shares":74000,"orphan":false,"hash":"8318949ba3ccb2f494c88984f97ceb7f7a874d8f80c7b5cb1849ee0d409c0e90","reward":"2345537715141"},{"height":291,"timestamp":1598371225,"difficulty":21600,"shares":8000,"orphan":false,"hash":"c2f3f4374bc470042514d7da88e65e473c1ccadb71f32766b071d21c75763938","reward":"2354538013391"},{"height":290,"timestamp":1598370036,"difficulty":21600,"shares":20000,"orphan":false,"hash":"8cac959e324abe7a59fc73a6c4362d5fd6fe970e9a7c3df8f0aa7ef1db2989c0","reward":"2354538311642"},{"height":289,"timestamp":1598369817,"difficulty":21600,"shares":10000,"orphan":false,"hash":"0428b8b54b991d2c457c688225d927c833eb7129940e2e5c7ae81f3f01ecf8b9","reward":"2351538609892"},{"height":288,"timestamp":1598369577,"difficulty":21600,"shares":2000,"orphan":false,"hash":"c348cf03b08328df2fd4974723adc3ea4e08574673be6dac3918b037b5283d2d","reward":"2350038908143"},{"height":287,"timestamp":1598369562,"difficulty":21600,"shares":20000,"orphan":false,"hash":"972b59578a388e63ea9957b566423aaecc3a4f507e75f77f71814cba3a941af9","reward":"2345539206394"},{"height":286,"timestamp":1598369478,"difficulty":21600,"shares":20000,"orphan":false,"hash":"7580458fe1e7da60b6c8e5bc80accf4eca92715ecbf631da6f73d81434e82c55","reward":"2357539504645"}],"maturedTotal":18}
```

#### 5) Host the frontend

Once `config.json` has "website"."enabled" set to true, it will listen by default locally on :8080 (or whichever port defined). It will leverage standard js/html/css files that a static webpage would, and integrate with the API above in #4.

website.go is the runner, which just starts the listenandserve on the port defined, then serves up content within /website/Pages , feel free to make modifications to folder structure, just be sure to update website.go

Credits
---------

* [sammy007](https://github.com/sammy007) - Developer on [monero-stratum](https://github.com/sammy007/monero-stratum) project from which current project is forked.
* [JKKGBE](https://github.com/JKKGBE) - Developer on [open-zcash-pool](https://github.com/JKKGBE/open-zcash-pool) which is forked from [sammy007](https://github.com/sammy007) project [open-ethereum-pool](https://github.com/sammy007/open-ethereum-pool)