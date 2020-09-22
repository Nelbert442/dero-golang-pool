package pool

type Config struct {
	PoolHost              string         `json:"poolHost"`
	BlockchainExplorer    string         `json:"blockchainExplorer"`
	TransactionExploer    string         `json:"transactionExplorer"`
	Address               string         `json:"address"`
	BypassShareValidation bool           `json:"bypassShareValidation"`
	Threads               int            `json:"threads"`
	Algo                  string         `json:"algo"`
	Coin                  string         `json:"coin"`
	CoinUnits             int64          `json:"coinUnits"`
	CoinDecimalPlaces     int64          `json:"coinDecimalPlaces"`
	CoinDifficultyTarget  int            `json:"coinDifficultyTarget"`
	TrustedSharesCount    int64          `json:"trustedSharesCount"`
	BlockRefreshInterval  string         `json:"blockRefreshInterval"`
	HashrateExpiration    string         `json:"hashrateExpiration"`
	UpstreamCheckInterval string         `json:"upstreamCheckInterval"`
	Upstream              []Upstream     `json:"upstream"`
	Stratum               Stratum        `json:"stratum"`
	API                   APIConfig      `json:"api"`
	UnlockerConfig        UnlockerConfig `json:"unlocker"`
	PaymentsConfig        PaymentsConfig `json:"payments"`
	Website               Website        `json:"website"`
}

type Upstream struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Timeout string `json:"timeout"`
	Enabled bool   `json:"enabled"`
}

type Stratum struct {
	PaymentID   PaymentID     `json:"paymentId"`
	FixedDiff   FixedDiff     `json:"fixedDiff"`
	WorkerID    WorkerID      `json:"workerID"`
	Timeout     string        `json:"timeout"`
	MaxFails    int64         `json:"maxFails"`
	HealthCheck bool          `json:"healthCheck"`
	Ports       []Port        `json:"listen"`
	VarDiff     VarDiffConfig `json:"varDiff"`
}

type PaymentID struct {
	AddressSeparator string `json:"addressSeparator"`
}

type FixedDiff struct {
	AddressSeparator string `json:"addressSeparator"`
}

type WorkerID struct {
	AddressSeparator string `json:"addressSeparator"`
}

type Port struct {
	Difficulty int64  `json:"diff"`
	MinDiff    int64  `json:"minDiff"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	MaxConn    int    `json:"maxConn"`
}

type VarDiffConfig struct {
	Enabled         bool    `json:"enabled"`
	MinDiff         int64   `json:"minDiff"`
	MaxDiff         int64   `json:"maxDiff"`
	TargetTime      int64   `json:"targetTime"`
	RetargetTime    int64   `json:"retargetTime"`
	VariancePercent float64 `json:"variancePercent"`
	MaxJump         float64 `json:"maxJump"`
}

type APIConfig struct {
	Enabled              bool   `json:"enabled"`
	Listen               string `json:"listen"`
	StatsCollectInterval string `json:"statsCollectInterval"`
	HashrateWindow       string `json:"hashrateWindow"`
	HashrateLargeWindow  string `json:"hashrateLargeWindow"`
	Blocks               int64  `json:"blocks"`
	Payments             int64  `json:"payments"`
}

type UnlockerConfig struct {
	Enabled        bool    `json:"enabled"`
	PoolFee        float64 `json:"poolFee"`
	Depth          int64   `json:"depth"`
	Interval       string  `json:"interval"`
	PoolFeeAddress string  `json:"poolFeeAddress"`
}

type PaymentsConfig struct {
	Enabled      bool   `json:"enabled"`
	Interval     string `json:"interval"`
	Mixin        uint64 `json:"mixin"`
	MaxAddresses uint64 `json:"maxAddresses"`
	Threshold    uint64 `json:"minPayment"`
	WalletHost   string `json:"walletHost"`
	WalletPort   string `json:"walletPort"`
}

type Website struct {
	Enabled bool   `json:"enabled"`
	Port    string `json:"port"`
}
