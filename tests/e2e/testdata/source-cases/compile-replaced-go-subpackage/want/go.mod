module demo-factory

go 1.26

require (
	github.com/cloudboss/unobin v0.1.0
	example.com/repo/go v0.0.0-unobin-replaced
)

replace (
	example.com/repo/go => <workspace>/module
)
