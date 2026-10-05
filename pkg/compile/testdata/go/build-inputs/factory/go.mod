module example.com/inputs/factory

go 1.26.2

require (
    example.com/inputs/library v0.1.0
    example.com/inputs/helper v0.1.0
    github.com/cloudboss/unobin v0.0.0
)

replace example.com/inputs/library => ../library
replace example.com/inputs/helper => ../helper
replace github.com/cloudboss/unobin => ../core
