package data

type AMI struct {
	ImageID string `mapstructure:"image-id"`
}

type AMIOutput struct {
	Architecture string `mapstructure:"architecture"`
}
