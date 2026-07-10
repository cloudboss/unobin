package diagnostic

import "sync"

type Collector struct {
	mu          sync.Mutex
	diagnostics []Diagnostic
}

func (c *Collector) Report(d Diagnostic) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diagnostics = append(c.diagnostics, cloneDiagnostic(d))
}

func (c *Collector) Diagnostics() []Diagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Normalize(c.diagnostics)
}

func (c *Collector) HasErrors() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}
