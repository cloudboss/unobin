module demo-factory

go 1.26

require (
	github.com/cloudboss/unobin v0.1.0
	example.com/lib/v2 v2.0.0-unobin-replaced
)

replace (
	example.com/lib/v2 => <workspace>/libv2
)
