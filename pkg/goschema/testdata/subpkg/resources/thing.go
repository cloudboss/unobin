package resources

type Thing struct {
	Name string
}

type ThingOutput struct {
	ID        string
	CidrBlock string
	Replicas  *int64
}
