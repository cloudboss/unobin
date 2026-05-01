package deps

// Go is the pinned Go toolchain `unobin compile` invokes when building
// stack binaries. Bumping the version means updating both the URL and
// the SHA256 for every supported platform.
var Go = Dependency{
	Name:    "go",
	Version: "1.26.2",
	Format:  TarGz,
	URLs: map[Platform]string{
		{"linux", "amd64"}: "https://go.dev/dl/go1.26.2.linux-amd64.tar.gz",
		{"linux", "arm64"}: "https://go.dev/dl/go1.26.2.linux-arm64.tar.gz",
		{"darwin", "amd64"}: "https://go.dev/dl/go1.26.2.darwin-amd64.tar.gz",
		{"darwin", "arm64"}: "https://go.dev/dl/go1.26.2.darwin-arm64.tar.gz",
	},
	SHA256: map[Platform]string{
		{"linux", "amd64"}:  "990e6b4bbba816dc3ee129eaeaf4b42f17c2800b88a2166c265ac1a200262282",
		{"linux", "arm64"}:  "c958a1fe1b361391db163a485e21f5f228142d6f8b584f6bef89b26f66dc5b23",
		{"darwin", "amd64"}: "bc3f1500d9968c36d705442d90ba91addf9271665033748b82532682e90a7966",
		{"darwin", "arm64"}: "32af1522bf3e3ff3975864780a429cc0b41d190ec7bf90faa661d6d64566e7af",
	},
	BinaryPath: "go/bin/go",
}

// All is the registry every test loops over to confirm coverage for
// the running platform.
var All = []Dependency{Go}
