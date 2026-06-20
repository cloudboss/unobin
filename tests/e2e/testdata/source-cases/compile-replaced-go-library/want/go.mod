module demo-factory

go 1.26

require (
	github.com/cloudboss/unobin v0.1.0
	github.com/cloudboss/unobin-library-aws v0.0.0-unobin-replaced
)

replace (
	github.com/cloudboss/unobin-library-aws => <workspace>/aws
)
