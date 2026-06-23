package shared

type Settings struct {
	Endpoint Endpoint
}

type Endpoint struct {
	URL  string `ub:"url"`
	Port int
}

type Retry struct {
	Count int
}
