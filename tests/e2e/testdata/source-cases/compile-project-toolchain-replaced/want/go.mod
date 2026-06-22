module demo-factory

go 1.26

require (
	github.com/cloudboss/unobin v0.1.0
)

replace (
	github.com/cloudboss/unobin => <workspace>/local-unobin
)
