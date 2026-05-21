package ports

type Port struct {
	Number   int64  `mapstructure:"number"`
	Protocol string `mapstructure:"protocol"`
}
