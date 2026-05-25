package resources

type Thing struct {
	Name string `mapstructure:"name"`
}

type ThingOutput struct {
	ID        string `mapstructure:"id"`
	CidrBlock string `mapstructure:"cidr-block"`
	Replicas  *int64 `mapstructure:"replicas"`
}
