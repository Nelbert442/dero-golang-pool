# dero-golang-pool
Golang Mining Pool for DERO

Default Frontend Stats (powershell): (invoke-webrequest -uri 'http://127.0.0.1:8082/stats').Content | ConvertFrom-Json

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

Copy the `config_examples/COIN.json` file of your choice to `config.json` then overview each options and change any to match your preferred setup.

Explanation for each field:
```javascript
{
    /*  Mining pool address */
	"address": "dEToUEe3q57XoqLgbuDE7DUmoB6byMtNBWtz85DmLAHAC8wSpetw4ggLVE4nB3KRMRhnFdxRT3fnh9geaAMmGrhP2UDY18gVNr",

    /*  True: Do not worry about verifying miner addresses, False: Validate miner addresses with built-in derosuite functions */
	"bypassAddressValidation": false,

    /*  True: Do not worry about verifying miner shares, False: Validate miner shares with built-in derosuite functions */
	"bypassShareValidation": false,

    /*  Number of threads to spawn stratum */
	"threads": 1,

    /*  Defines algorithm used by pool. References to this switch are in miner.go */
	"algo": "astrobwt",

    /*  Defines estimation window of a block being mined. Usually set to 'average' block speed */
	"estimationWindow": "27s",

    /*  Used for block stats */
	"luckWindow": "24h",

    /*  Used for block stats */
	"largeLuckWindow": "72h",

    /*  Defines how often the upstream (daemon) getblocktemplate is refreshed.
        DERO blockchain is fast and runs on 27 Seconds blocktime. Best practice is to update your mining job at-least every second. 
        Bitcoin pool also updates miner job every 10 seconds and BTC blocktime is 10 mins -Captain [03/08/2020] .
        Example of 10 second updates for 10 minute blocktimes on BTC. ~10/600 * 27 = 0.45 */
	"blockRefreshInterval": "450ms",

	"stratum": {
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

	"frontend": {
		"enabled": true,            // Set frontend enabled to true, self-hosted frontend, or false, not hosted
		"listen": "0.0.0.0:8082",   // Set bind address and port for frontend
		"login": "admin",           // Set login username for frontend
		"password": "",             // Set password for frontend
		"hideIP": false             // Set hideIP on whether or not to show miner IPs on the frontend or not
	},

	"upstreamCheckInterval": "5s",  // How often to poll upstream (daemon) for successful connections

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

	"redis": {
		"enabled": true,            // Set redis enabled to true, utilized, or false, not utilized
		"host": "172.28.89.248",    // Set address to reach redis db over
		"port": 6379,               // Set port to append to host
		"password": "",             // Set password for db access
		"DB": 0                     // Set index of db
	}
}
```