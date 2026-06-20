module demo-factory

go 1.26

require (
	github.com/cloudboss/unobin v0.0.0-unobin-replaced
	example.com/unobin/e2elib v0.0.0-unobin-replaced
)

replace (
	example.com/unobin/e2elib => <workspace>/modules/e2elib
	github.com/cloudboss/unobin => <repo>
)
