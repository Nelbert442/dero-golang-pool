package pool

type Config struct {
	Address                 string     `json:"address"`
	BypassAddressValidation bool       `json:"bypassAddressValidation"`
	BypassShareValidation   bool       `json:"bypassShareValidation"`
	Threads                 int        `json:"threads"`
	Algo                    string     `json:"algo"`
	Coin                    string     `json:"coin"`
	TrustedSharesCount      int64      `json:"trustedSharesCount"`
	BlockRefreshInterval    string     `json:"blockRefreshInterval"`
	UpstreamCheckInterval   string     `json:"upstreamCheckInterval"`
	Upstream                []Upstream `json:"upstream"`
	Stratum                 Stratum    `json:"stratum"`
	API                     API        `json:"api"`
	Redis                   Redis      `json:"redis"`
}

type Upstream struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Timeout string `json:"timeout"`
	Enabled bool   `json:"enabled"`
}

type Stratum struct {
	Timeout string `json:"timeout"`
	Ports   []Port `json:"listen"`
}

type Port struct {
	Difficulty int64  `json:"diff"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	MaxConn    int    `json:"maxConn"`
}

type API struct {
	Enabled          bool   `json:"enabled"`
	Listen           string `json:"listen"`
	Login            string `json:"login"`
	Password         string `json:"password"`
	HideIP           bool   `json:"hideIP"`
	EstimationWindow string `json:"estimationWindow"`
	LuckWindow       string `json:"luckWindow"`
	LargeLuckWindow  string `json:"largeLuckWindow"`
}

type Redis struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	DB       int    `json:"DB"`
}
