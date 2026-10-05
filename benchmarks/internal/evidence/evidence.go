package evidence

import "time"

const FormatVersion = 1
const BenchstatVersion = "v0.0.0-20260929162123-406019bb8b68"

type Spec struct {
	WorkPackage      string            `json:"work-package"`
	Description      string            `json:"description"`
	Kind             string            `json:"kind"`
	Fixtures         []string          `json:"fixtures"`
	Parameters       map[string]string `json:"parameters"`
	Packages         []string          `json:"packages"`
	Benchmark        string            `json:"benchmark"`
	Cases            []Case            `json:"cases"`
	Samples          int               `json:"samples"`
	Benchtime        string            `json:"benchtime"`
	CPUs             []int             `json:"cpus"`
	Setup            string            `json:"setup"`
	CachePolicy      string            `json:"cache-policy"`
	PrimaryUnit      string            `json:"primary-unit"`
	PrimaryDirection string            `json:"primary-direction"`
	Tests            [][]string        `json:"tests"`
}

type Case struct {
	Package string             `json:"package"`
	Name    string             `json:"name"`
	Units   []string           `json:"units"`
	Fixed   map[string]float64 `json:"fixed,omitempty"`
}

type Machine struct {
	GoVersion   string `json:"go-version"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	CPUModel    string `json:"cpu-model"`
	LogicalCPUs int    `json:"logical-cpus"`
}

type Run struct {
	Args      []string  `json:"args"`
	Directory string    `json:"directory"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	ExitCode  int       `json:"exit-code"`
	Output    string    `json:"output"`
	Digest    string    `json:"sha256"`
	Error     string    `json:"error,omitempty"`
}

type Record struct {
	FormatVersion   int               `json:"format-version"`
	Spec            Spec              `json:"workload"`
	Revision        string            `json:"source-revision"`
	FixtureRevision string            `json:"fixture-revision"`
	SpecPath        string            `json:"workload-path"`
	Repository      string            `json:"repository"`
	CleanSource     bool              `json:"clean-source"`
	Fixtures        map[string]string `json:"fixture-digests"`
	Dependencies    map[string]string `json:"dependency-locks"`
	Machine         Machine           `json:"machine"`
	Environment     map[string]string `json:"environment"`
	Capabilities    []string          `json:"capabilities,omitempty"`
	Benchstat       string            `json:"benchstat-version"`
	Tests           []Run             `json:"tests"`
	Warmup          Run               `json:"warmup"`
	Benchmark       Run               `json:"benchmark"`
}

type Key struct {
	Package string
	Name    string
	CPU     int
	Unit    string
}

type Measurements map[Key][]float64

type Distribution struct {
	Samples    int      `json:"samples"`
	Median     float64  `json:"median"`
	Low        *float64 `json:"low"`
	High       *float64 `json:"high"`
	Confidence float64  `json:"confidence"`
}

type Statistic struct {
	Package      string       `json:"package"`
	Name         string       `json:"name"`
	CPU          int          `json:"cpu"`
	Unit         string       `json:"unit"`
	Before       Distribution `json:"before"`
	After        Distribution `json:"after"`
	DeltaPercent *float64     `json:"delta-percent"`
	P            float64      `json:"p"`
	Alpha        float64      `json:"alpha"`
	Significant  bool         `json:"significant"`
	Assessment   string       `json:"assessment"`
	Warnings     []string     `json:"warnings"`
}

type Summary struct {
	FormatVersion   int         `json:"format-version"`
	WorkPackage     string      `json:"work-package"`
	BeforeRevision  string      `json:"before-revision"`
	AfterRevision   string      `json:"after-revision"`
	FixtureRevision string      `json:"fixture-revision"`
	PrimaryUnit     string      `json:"primary-unit"`
	Assessment      string      `json:"assessment"`
	Statistics      []Statistic `json:"statistics"`
	Warnings        []string    `json:"warnings"`
}
