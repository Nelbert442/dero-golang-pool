package pool

type Config struct {
	Address                 string         `json:"address"`
	BypassAddressValidation bool           `json:"bypassAddressValidation"`
	BypassShareValidation   bool           `json:"bypassShareValidation"`
	Threads                 int            `json:"threads"`
	Algo                    string         `json:"algo"`
	Coin                    string         `json:"coin"`
	TrustedSharesCount      int64          `json:"trustedSharesCount"`
	BlockRefreshInterval    string         `json:"blockRefreshInterval"`
	HashrateExpiration      string         `json:"hashrateExpiration"`
	UpstreamCheckInterval   string         `json:"upstreamCheckInterval"`
	Upstream                []Upstream     `json:"upstream"`
	Stratum                 Stratum        `json:"stratum"`
	API                     APIConfig      `json:"api"`
	Redis                   Redis          `json:"redis"`
	UnlockerConfig          UnlockerConfig `json:"unlocker"`
}

type Upstream struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Timeout string `json:"timeout"`
	Enabled bool   `json:"enabled"`
}

type Stratum struct {
	PaymentID PaymentID `json:"paymentId"`
	FixedDiff FixedDiff `json:"fixedDiff"`
	WorkerID  WorkerID  `json:"workerID"`
	Timeout   string    `json:"timeout"`
	Ports     []Port    `json:"listen"`
}

type PaymentID struct {
	AddressSeparator string   `json:"addressSeparator"`
	Validation       bool     `json:"validation"`
	Validations      []string `json:"validations"`
	Ban              bool     `json:"ban"`
}

type FixedDiff struct {
	Enabled          bool   `json:"enabled"`
	AddressSeparator string `json:"addressSeparator"`
}

type WorkerID struct {
	Enabled          bool   `json:"enabled"`
	AddressSeparator string `json:"addressSeparator"`
}

type Port struct {
	Difficulty int64  `json:"diff"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	MaxConn    int    `json:"maxConn"`
}

type APIConfig struct {
	Enabled              bool   `json:"enabled"`
	PurgeOnly            bool   `json:"purgeOnly"`
	PurgeInterval        string `json:"purgeInterval"`
	Listen               string `json:"listen"`
	Login                string `json:"login"`
	Password             string `json:"password"`
	HideIP               bool   `json:"hideIP"`
	StatsCollectInterval string `json:"statsCollectInterval"`
	HashrateWindow       string `json:"hashrateWindow"`
	HashrateLargeWindow  string `json:"hashrateLargeWindow"`
	LuckWindow           []int  `json:"luckWindow"`
	Blocks               int64  `json:"blocks"`
}

type Redis struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	DB       int64  `json:"DB"`
}

type UnlockerConfig struct {
	Enabled        bool    `json:"enabled"`
	PoolFee        float64 `json:"poolFee"`
	Depth          int64   `json:"depth"`
	Interval       string  `json:"interval"`
	PoolFeeAddress string  `json:"poolFeeAddress"`
}
