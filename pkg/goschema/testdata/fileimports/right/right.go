package right

type Record struct {
	Number int `ub:"count"`
}

type Result struct {
	Enabled bool
	Secret  string `ub:",sensitive"`
}
