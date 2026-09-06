package runtime

// RefreshResult reports observed resources, removed resources, and the saved revision.
type RefreshResult struct {
	WrittenRev string
	Refreshed  int
	Dropped    int
}
