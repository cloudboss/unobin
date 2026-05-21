package endpoints

import "example.com/crosspkg/ports"

type Endpoint struct {
	Host string     `mapstructure:"host"`
	Port ports.Port `mapstructure:"port"`
}
