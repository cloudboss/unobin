package types

type Predicate func() (bool, error)

type Action func() *Result

type Result struct {
	Succeeded bool                   `json:"succeeded"`
	Changed   bool                   `json:"changed"`
	Error     string                 `json:"error,omitempty"`
	Module    string                 `json:"module"`
	Output    map[string]interface{} `json:"output,omitempty"`
}

type Context struct {
	Vars  map[string]interface{}
	State map[string]interface{}
	Item  interface{}
}

func DoIf(module string, condition Predicate, do Action) *Result {
	done, err := condition()
	if err != nil {
		return &Result{
			Succeeded: false,
			Changed:   false,
			Error:     err.Error(),
			Module:    module,
		}
	}
	if !done {
		return do()
	}
	return &Result{
		Succeeded: true,
		Changed:   false,
		Module:    module,
	}
}
