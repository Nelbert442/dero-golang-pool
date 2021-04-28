package pool

type Config struct {
	PoolHost                string           `json:"poolHost"`
	BlockchainExplorer      string           `json:"blockchainExplorer"`
	TransactionExploer      string           `json:"transactionExplorer"`
	Address                 string           `json:"address"`
	DonationAddress         string           `json:"donationAddress"`
	DonationDescription     string           `json:"donationDescription"`
	BypassShareValidation   bool             `json:"bypassShareValidation"`
	Threads                 int              `json:"threads"`
	Algo                    string           `json:"algo"`
	Coin                    string           `json:"coin"`
	CoinUnits               int64            `json:"coinUnits"`
	CoinDecimalPlaces       int64            `json:"coinDecimalPlaces"`
	CoinDifficultyTarget    int              `json:"coinDifficultyTarget"`
	TrustedSharesCount      int64            `json:"trustedSharesCount"`
	BlockRefreshInterval    string           `json:"blockRefreshInterval"`
	HashrateExpiration      string           `json:"hashrateExpiration"`
	StoreMinerStatsInterval string           `json:"storeMinerStatsInterval"`
	GravitonMaxSnapshots    uint64           `json:"gravitonMaxSnapshots"`
	GravitonMigrateWait     string           `json:"gravitonMigrateWait"`
	UpstreamCheckInterval   string           `json:"upstreamCheckInterval"`
	Upstream                []Upstream       `json:"upstream"`
	Stratum                 Stratum          `json:"stratum"`
	API                     APIConfig        `json:"api"`
	UnlockerConfig          UnlockerConfig   `json:"unlocker"`
	PaymentsConfig          PaymentsConfig   `json:"payments"`
	Website                 Website          `json:"website"`
	PoolCharts              PoolChartsConfig `json:"poolcharts"`
	SoloCharts              SoloChartsConfig `json:"solocharts"`
	EventsConfig            EventsConfig     `json:"events"`
}

type Upstream struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Timeout string `json:"timeout"`
	Enabled bool   `json:"enabled"`
}

type Stratum struct {
	PaymentID     PaymentID     `json:"paymentId"`
	FixedDiff     FixedDiff     `json:"fixedDiff"`
	WorkerID      WorkerID      `json:"workerID"`
	DonatePercent DonatePercent `json:"donatePercent"`
	SoloMining    SoloMining    `json:"soloMining"`
	Timeout       string        `json:"timeout"`
	MaxFails      int64         `json:"maxFails"`
	HealthCheck   bool          `json:"healthCheck"`
	Ports         []Port        `json:"listen"`
	VarDiff       VarDiffConfig `json:"varDiff"`
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

type DonatePercent struct {
	AddressSeparator string `json:"addressSeparator"`
}

type SoloMining struct {
	Enabled          bool   `json:"enabled"`
	AddressSeparator string `json:"addressSeparator"`
}

type Port struct {
	Difficulty int64  `json:"diff"`
	MinDiff    int64  `json:"minDiff"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	MaxConn    int    `json:"maxConn"`
	Desc       string `json:"desc"`
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
	Payments             int64  `json:"payments"`
	Blocks               int64  `json:"blocks"`
	SSL                  bool   `json:"ssl"`
	SSLListen            string `json:"sslListen"`
	CertFile             string `json:"certFile"`
	KeyFile              string `json:"keyFile"`
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
	Enabled  bool   `json:"enabled"`
	Port     string `json:"port"`
	SSL      bool   `json:"ssl"`
	SSLPort  string `json:"sslPort"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type PoolChartsConfig struct {
	Interval   int64           `json:"interval"`
	Hashrate   ChartDataConfig `json:"hashrate"`
	Miners     ChartDataConfig `json:"miners"`
	Workers    ChartDataConfig `json:"workers"`
	Difficulty ChartDataConfig `json:"difficulty"`
}

type SoloChartsConfig struct {
	Interval int64           `json:"interval"`
	Hashrate ChartDataConfig `json:"hashrate"`
	Miners   ChartDataConfig `json:"miners"`
	Workers  ChartDataConfig `json:"workers"`
}

type ChartDataConfig struct {
	Enabled       bool  `json:"enabled"`
	MaximumPeriod int64 `json:"maximumPeriod"`
}

type EventsConfig struct {
	Enabled                 bool                    `json:"enabled"`
	RandomRewardEventConfig RandomRewardEventConfig `json:"randomrewardevent"`
}

type RandomRewardEventConfig struct {
	Enabled               bool    `json:"enabled"`
	StartDay              string  `json:"startDay"`
	EndDay                string  `json:"endDay"`
	StepIntervalInSeconds int64   `json:"stepIntervalInSeconds"`
	RewardValueInDERO     int64   `json:"rewardValueInDERO"`
	MinerPercentCriteria  float64 `json:"minerPercentCriteria"`
}
