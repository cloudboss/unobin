package endpoints

import "example.com/crosspkg/ports"

type Endpoint struct {
	Host string
	Port ports.Port
}
