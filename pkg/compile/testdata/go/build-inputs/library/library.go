package library

import (
	_ "embed"

	"example.com/inputs/helper"

	"example.com/inputs/library/engine"
)

//go:embed data/message.txt
var message string

func Value() string { return message + helper.Value() + engine.Value() }
